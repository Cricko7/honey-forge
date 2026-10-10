# Ловушки: жизненный цикл и реквизиты агента

Реализация `docs/api/05-traps.md` подключена к production composition root
`internal/app.Open`: CRUD `/api/traps`, три маршрута `agent-credentials`, команды
06 и настоящий агентский gateway 08. Публичный контракт — `api/openapi.yaml` в
корне репозитория.

`revision` и `updated_at` меняются только при фактическом изменении имени или
описания. Пустой PATCH отклоняется, no-op не пишет аудит. `state_version`
изменяется вместе с полным DTO. Heartbeat не конфликтует с
`X-Expected-Revision`; время берётся у сервера. Результат команды и телеметрия
не заменяют heartbeat. Disconnect сохраняет последнее runtime-состояние.

PostgreSQL сериализует редактирование, команды, реквизиты и tombstone через
журнал организации и блокировку строки ловушки. Условия DELETE считываются
отдельным SQL-запросом после получения блокировки: это учитывает ingestion,
который успел завершить резервирование во время ожидания DELETE. Ловушка,
которой никогда не выдавались реквизиты, допускает удаление offline; все
остальные требуют online/stopped, здоровый нулевой буфер и отсутствие
активной команды/незавершённого ingestion.

Токен — 32 случайных байта, PostgreSQL хранит только SHA-256. GET не возвращает
секрет; первая выдача требует `expected_generation=0`, повтор старого значения
получает 409. ETag реквизитов отделён от revision ловушки. Отзыв/замена/удаление
закрывают старый WSS с 4401. Generation и connection_id повторно проверяются
под транзакционной блокировкой при наблюдениях, командах и приёме событий.
Отзыв реквизитов не означает stop.

`AGENT_WS_URL` задаёт публичный `wss://.../assets/stream`. По умолчанию адрес
строится из первого `BROWSER_ORIGINS`. Он никогда не строится из hostname агента.

Для модуля 07 доступны `BeginIngestion`, `IngestionTrap`, `FinishIngestion` и
граница `Ingester`. Резервирование сохраняется в БД до подтверждённого commit
или окончательного отказа; неоднозначная ошибка оставляет блокировку удаления
до reconciliation. Ротация token не удаляет эти записи. По умолчанию подключён
`modules/events`: атомарный PostgreSQL-приём, дедупликация и чтение истории.
Внешний ingester можно передать через `app.Config.Ingester`; он обязан соблюдать
тот же контракт, включая завершение reservation после фоновой материализации.

Tombstone сохраняет строку, организацию, тип, snapshots и историю. Собственные
GET commands и events используют tombstone; остальные маршруты удалённой
ловушки возвращают 404. Аудит, notification и пометка использованного request_id
фиксируются вместе с удалением. Новая регистрация получает новый ID.

Планировщик приложения переводит протухшие heartbeat в offline и завершает
offline-команды по сроку 24 часа. Отдельный процесс агента отвечает за listeners,
свой устойчивый журнал и локальный буфер; backend не разворачивает и не удаляет
процессы на хосте агента.

Проверки из `src/backend`:

```powershell
go test ./modules/traps ./modules/events ./modules/agentws ./modules/commands/...
go test -tags=integration ./internal/app -run 'TestTrap|TestConcurrentStart'
go fmt ./...
go vet ./...
go test ./...
go build ./...
go test -race ./...
go test -race -tags=integration ./...
```

PostgreSQL-тесты требуют `TEST_DATABASE_URL`, создают отдельную схему и
применяют все миграции с нуля. Они проверяют права viewer/чужой ID, CSRF,
generation/ETag, переполнение, heartbeat/PATCH, pending ingestion, гонки,
сохранение истории и настоящий TLS/WSS с close 4401.
