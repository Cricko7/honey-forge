# Команды ловушек

Модуль реализует правила [06-commands.md](../../docs/api/06-commands.md): `apply_config`, `start`, `stop`, очередь и чтение истории. `delete` принадлежит модулю ловушек 05; `status` и `info` читаются через GET. Восстановление удалённой ловушки контракт не предусматривает.

## Что реализовано

- `service.Service` проверяет роль admin/viewer, входные данные и действия точной версии каталога. `start` требует применённую конфигурацию; `stop` возможен до первой конфигурации. `apply_config` закрепляет полную текущую ревизию профиля внутри транзакции. Snapshot не входит в ответ оператора.
- `repository.Repository` записывает команду через общий `mutation.Store`: request_id, команда, изменение ловушки, аудит `command.created` и уведомления фиксируются вместе. Повтор request_id проверяется до блокировки новой активной команды. Уникальный частичный индекс запрещает вторую queued/running команду.
- Replay создания возвращает первоначальный `queued` DTO даже после исполнения; актуальный результат читается GET. Метки времени ответов приведены к UTC, точность dispatch/ack совпадает с микросекундами PostgreSQL, поэтому повторный `command.ack` сохраняет исходный `recorded_at`.
- `ExpireDue` завершает просроченную команду и освобождает ловушку; вызывается при новом POST и выдаче агенту. Планировщик приложения вызывает `ExpireAllDue` также для offline-ловушек без новых запросов.
- `service.Agent` и repository выдают lease на 60 секунд, продлевают его и фиксируют результат. Повтор одинакового terminal result возвращает прежний ack без повторной записи. Старый lease и конфликт результата отвергаются. Ошибки агента приводятся к безопасному тексту по коду.
- Продление lease, выдача и результат используют одну транзакционную проверку generation/connection_id под блокировкой ловушки. Старое соединение не продлевает lease после замены сессии или token.
- HTTP handler предоставляет POST/GET `/api/traps/{id}/commands` и GET `/api/traps/{id}/commands/{command_id}`. Admin создаёт, admin/viewer читают; список поддерживает `limit`, `cursor`, `status`.

## Подключение к модулю 05

Модуль ловушек подключён: `modules/traps.Repository` реализует `TrapBoundary`,
`internal/app.Open` регистрирует HTTP-маршруты с настоящей сессией, Origin/CSRF
и агентский сервис. DELETE и создание команды сериализованы одной строкой
ловушки. FK в PostgreSQL связывает команду с ловушкой той же организации.

Граница `repository.TrapBoundary` получает ту же `pgx.Tx`, что и изменение команды: `Lock` захватывает живую ловушку, `Activate` атомарно задаёт `active_command_id` и desired state/revision, `Release` снимает активную просроченную команду, `Complete` атомарно записывает подтверждённый runtime/applied revision и снимает active ID. `Visible` допускает чтение истории своей tombstone-ловушки. Порядок блокировок: журнал организации → ловушка → профиль/команда. DELETE ловушки использует тот же порядок и отвергает активную команду.

Адаптеры каталога находятся в `internal/app/command_catalog.go`. Агентский WSS вызывает `Claim`, `ExtendLease`, `RecordResult` после проверки активного `connection_id`, generation и поддерживаемых type/action. Он отправляет dispatch/ack; исполнение команды на хосте остаётся обязанностью отдельного процесса агента.

`mutation_changes` хранит безопасные идентификаторы и state_version. Полное сообщение `command.changed` для frontend WSS собирает модуль 09 при интеграции; секретный snapshot туда не передаётся. Три маршрута и схемы Command внесены в `api/openapi.yaml`; пометка ожидания модуля 05 снята. `ExpireAllDue` вызывается планировщиком приложения и завершает offline-команды даже без новых запросов.

## Проверки

Из `src/backend`:

```powershell
go test ./modules/commands/...
go test -tags=integration ./modules/commands/repository -run '^TestPostgresCommandLifecycle$' -count=1
go fmt ./...
go vet ./...
go test ./...
go build ./...
```

Тесты service и HTTP используют небольшие фейки хранилища, snapshot и каталога. PostgreSQL-тест с `TEST_DATABASE_URL` создаёт отдельную схему, применяет миграции и проверяет очередь, replay, конфликт, истечение срока, lease, результат и аудит через временную реализацию границы ловушки. Тесты `internal/app` дополнительно проверяют полный жизненный цикл с настоящей ловушкой, историю после tombstone и конкурентные start/DELETE.

## Полный сценарий traps + commands + WSS

Нужен запущенный PostgreSQL и `TEST_DATABASE_URL` с правами на создание схем.
Из `src/backend`:

```powershell
go test -tags=integration ./internal/app -run '^TestTrapCommandsWSSLifecycle$' -count=1 -v
go test -tags=integration ./internal/app -run '^TestTrapCommand' -count=1 -v
```

`TestTrapCommandsWSSLifecycle` запускает production composition root на свежей
схеме и настоящий TLS/WSS сервер. Агент имитируется клиентом протокола;
gateway, команды, ловушки, профили, каталог и ingestion используют реальные
реализации и PostgreSQL. Отдельный процесс агента не требуется.

Сценарий: создание профиля/ловушки → отказ start без конфигурации → offline
очередь apply_config → редактирование Profile → hello/dispatch с первоначальным
snapshot → progress → отказ некорректного результата → результат/повторный ack
→ reconnect с установленным snapshot → start с прежней revision → неудачный
stop → повторный успешный stop → отказ DELETE при заполненном буфере →
telemetry/ack → heartbeat с пустым буфером → DELETE и WSS close 4401 → чтение
истории владельцем и viewer, отказ чужой организации и запрет записи viewer.

Остальные тесты `TestTrapCommand*` проверяют отзыв токена во время lease,
первоначальный replay при наличии новой активной команды, истечение offline
очереди и полный rollback результата при переполнении `state_version`.
