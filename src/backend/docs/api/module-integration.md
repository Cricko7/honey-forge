# Связывание модулей HoneyForge и независимое тестирование

## Как пользоваться

Каждый участник может взять один модуль, написать его и проверить с моками соседних модулей. Готовность соседней реализации для этого не нужна. До начала работы команда фиксирует перечисленные ниже операции, входы, результаты и ошибки; потребитель и поставщик используют один договор. Затем в сборке моки заменяются реальными зависимостями без изменения бизнес-логики и HTTP/WSS-контракта.

Этот документ дополняет [порядок работы](README.md) и [приёмку](11-happy-path.md). Он описывает внутренние связи, не добавляет публичных маршрутов и не заменяет правила модулей 01–11. Контракт остаётся в Markdown, без YAML. Здесь задан договор для будущего кода; сами интерфейсы, моки и тесты ещё не реализованы.

## Что согласовать перед разделением задач

1. Общие ID, Timestamp, Revision, ошибки, Page, Envelope и preconditions — [01](01-common.md).
2. Источник user_id, organization_id и role — [02](02-auth-organizations.md). Клиентские значения не заменяют серверный контекст доступа.
3. Схемы на каждой границе — таблица ниже. Не создавать второй вариант Profile/Trap/Event с другими полями.
4. Названия и входы/выходы внутренних операций — раздел «Операции на границах». Поставщик должен удовлетворять потребляемому интерфейсу; изменения согласуются с потребителями в том же изменении.
5. Общая фиксация записей — раздел «Атомарные операции». Проверка через моки не заменяет транзакцию при подключении PostgreSQL.

## Владельцы данных и границы пакетов

| Модуль | Его данные и ответственность | Что отдаёт другим |
|---|---|---|
| [01](01-common.md) | Общие транспортные правила | Единые ошибки/ответы, лимиты, Envelope, правила preconditions |
| [02](02-auth-organizations.md) | Оператор, организация, сессия, join-code | AuthContext и проверка активной сессии |
| [03](03-catalog.md) | Immutable-версии типов и схем | CatalogEntry и проверка config/event/params/result |
| [04](04-profiles.md) | Profile и версии конфигурации | Данные профиля и полный snapshot заданной revision |
| [05](05-traps.md) | Trap, привязка, credentials, наблюдаемое состояние, tombstone | Проверка принадлежности, agent identity, изменение состояния |
| [06](06-commands.md) | Command, request_id, очередь, lease, закреплённый snapshot | Создание/выдача/фиксация команды и её история |
| [07](07-events.md) | Event, дедупликация, ingestion, журнал изменений и stream_cursor | Сохранение batch, REST-события, чтение журнала изменений |
| [08](08-agent-ws.md) | Соединение агента и WSS-протокол | Связь транспорта с 05/06/07; сообщения hello/heartbeat/result/batch |
| [09](09-frontend-ws.md) | Соединение frontend, replay, backpressure | Уведомления из журнала 07, без повторного сохранения Event |
| [10](10-audit.md) | Неизменяемые AuditEntry | Запись безопасного аудита внутри бизнес-транзакции и его чтение |
| [11](11-happy-path.md) | Сквозная приёмка | Сценарии проверки общей сборки; это не отдельный runtime-модуль |

Feature-пакеты владеют своими Go-типами. Интерфейс объявляется у потребителя и содержит только нужные ему методы. Зависимость передаётся явно при сборке приложения; DI-фреймворк и общий пакет models/common не нужны. SQL остаётся в repository, Gin — в HTTP-слое.

Не делать взаимные импорты 04↔05 или 05↔06. Для узкой проверки достаточно результата вроде «привязка есть/нет»; при несовпадении типов маленький адаптер соединяет границы в internal/app. Например, публичная схема ConfigurationSnapshot описана в 06, но полный снимок конфигурации принадлежит профилям 04: 04 не импортирует команды ради возврата их типа, 06/адаптер преобразует результат 04 в ConfigurationSnapshot без потери полей. Аналогично 06 принимает наблюдение runtime через свой потребляемый договор, не импортируя транспорт 08.

## Общие входы внутренних операций

Каждый вызов получает context отмены. В таблицах он подразумевается, но в Go-сигнатуре присутствует явно. ID и время имеют смысл из 01; JSON-объекты не подменяются строками JSON.

- AuthContext: user_id, organization_id, role. Создаётся после проверки operator-сессии. Сервис отдельно проверяет роль и организацию до side effect.
- AgentIdentity: trap_id, organization_id, type_id, type_version, credential_generation. Создаётся по активному token, не из payload агента.
- AgentConnection: AgentIdentity и connection_id активного hello-соединения. Старое/заменённое соединение не может менять состояние.
- MutationScope: действующая транзакция конкретного хранилища для связанных записей. Это технический контекст фиксации, не поле HTTP и не глобальный generic repository.

Последние два договора нужны для fencing соединений и общей фиксации; их техническое представление команда фиксирует до работы с БД. AuthContext/AgentIdentity не содержат пароли и значения tokens.

## Операции на границах

Имена ниже — согласованные имена внутренних операций, а не URL. При переводе в Go потребитель фиксирует точные сигнатуры и типы в своём узком интерфейсе до реализации обеих сторон. Без этого нельзя считать два независимо написанных пакета автоматически совместимыми.

| Потребитель → поставщик | Операция и вход | Результат | Ошибка / важное правило |
|---|---|---|---|
| Operator middleware/09 → 02 | ResolveSession(cookie) | AuthContext, session audit id, expires_at | Неактивная сессия — unauthenticated; cookie не раскрывается |
| Operator middleware → 02 | CheckCSRF(session, token) | Успех | csrf_failed; Origin проверяется отдельно по 01/02 |
| 04/06/07 → 03 | LookupType(type_id, type_version) | Точный CatalogEntry | Неизвестная пара; HTTP-слой отображает 404 каталога или 422 unknown_trap_type при создании профиля |
| 04 → 03 | CheckConfig(entry, config) | Успех / безопасные FieldError | config_invalid/config_too_large; финальный config включает сохранённые секреты |
| 06 → 03 | CheckAction(entry, action, params/result) | Descriptor и успешная валидация | unsupported_action/command_params_invalid; result проверяется отдельно перед успехом |
| 07 → 03 | CheckEvent(entry, event_type, data) | Успех / безопасные FieldError | telemetry_invalid; тип события обязан быть объявлен |
| 05 → 04 | ReadProfile(organization_id, profile_id) | Свой профиль, type/version | Чужой/отсутствующий — resource_not_found |
| 06 → 04 | CaptureProfile(organization_id, profile_id, revision, scope) | Полный immutable snapshot | profile_changed; снимок закрепляется в той же операции создания Command |
| 04 → 05 | HasLiveBindings(organization_id, profile_id, scope) | Да/нет | Проверка и DELETE Profile в одной транзакции; удалённые Trap не считаются |
| 06/07 → 05 | ReadTrap(organization_id, trap_id, include_deleted) | Данные своей Trap/снимков или tombstone | Для новой команды include_deleted=false; для истории true; чужой id всегда скрыт |
| 08 → 05 | ResolveAgent(token) | AgentIdentity | agent_unauthenticated; operator-cookie не принимается |
| 08 → 05 | OpenConnection(identity, hello) | AgentConnection и данные welcome | Новый hello заменяет прежнее соединение; unknown_configuration/unsupported_type |
| 08 → 05 | ObserveRuntime(connection, runtime) | Новое состояние Trap | Проверка активного token/connection; меняется state_version, не revision |
| 05 → 06 | HasActiveCommand(trap_id, scope) | Да/нет | Используется для DELETE под общей блокировкой Trap |
| 05 → 07 | HasPendingIngestion(trap_id, scope) | Да/нет | Учтены принятые, но ещё не материализованные batches, включая Kafka; не только текущий вызов WSS |
| 06 → 05 | UpdateCommandState(trap_id, desired/applied/runtime/active_command_id, scope) | Обновлённая Trap | Не отдельный commit от Command; heartbeat не увеличивает revision |
| 08 → 06 | ClaimCommand(connection) | Dispatch или отсутствие команды | Новый lease, проверка tombstone/поддержки; та же команда после reconnect |
| 08 → 06 | ExtendLease(connection, command_id, lease_id) | Новое lease_expires_at | stale_command_lease/command_expired |
| 08 → 06 | RecordResult(connection, command_id, lease_id, status, result, error, runtime) | Данные command.ack | Ack после commit; старый result не затирает актуальный runtime; повторы по правилам 08 |
| 08 → 07 | IngestBatch(connection, batch_id, events) | TelemetryAck для telemetry.ack после сохранения всей пачки | pending/busy/unavailable/conflict по 07/08; при ошибке нет положительного ack |
| 09 → 07 | ReadChanges(organization_id, after, limit) | Упорядоченные изменения и граница replay | cursor_expired/invalid_cursor; нет данных чужой организации |
| 02/04/05/06/07/10 → 07 | AppendChange(scope, organization_id, type, safe_data) | Устойчивое уведомление/cursor | Общая фиксация с изменением; не сетевой send до commit |
| 02/04/05/06 → 10 | AppendAudit(scope, actor, action, resource, details) | AuditEntry | Только разрешённые metadata; ошибка записи не оставляет успешную бизнес-операцию без audit |

ReadChanges возвращает DTO уведомлений из 09: cursor, occurred_at, type и data. Это чтение устойчивого журнала, а не уведомления из памяти. Для незавершённого восстановления batch идентификатор и исходное содержимое сохраняются. ResolveSession и активность агента перепроверяются по правилам WSS до передачи данных, не только при первом подключении.

## Что мокать и проверять в каждом модуле

Мок заменяет соседнюю зависимость, не бизнес-логику проверяемого модуля. Нужен небольшой fake с заданными ответами/ошибками и записью вызовов; mocking framework не обязателен. Фейковая зависимость всегда реализует тот же потребляемый интерфейс, что и реальный адаптер.

| Модуль | Моки / изолированное окружение | Минимальная собственная проверка |
|---|---|---|
| 01 | Минимальный Gin router; сессия не нужна для правил JSON | Неверный JSON 400, бизнес-валидация 422, лимит 413, media type 415, единый error envelope |
| 02 | Свой repository, clock, audit writer, change writer | Create/join/login/logout, дубликат email, старый join-code, неверные credentials, Origin/CSRF; отказ записи audit откатывает изменение |
| 03 | AuthContext admin/viewer, небольшой каталог | Точная версия, неизвестный тип, immutable schemas, config/event/action validation |
| 04 | Каталог, проверка привязок 05, audit/change writers, свой repository | Create/PATCH/DELETE, неверный config, 412, writeOnly, profile_in_use, viewer/чужой id |
| 05 | Профили, active command, pending ingestion, clock, audit/change writers | PATCH между heartbeat, stale revision, credentials, stop-состояние, DELETE offline/running/с буфером, tombstone и отзыв token |
| 06 | Каталог, snapshots, граница Trap, clock, audit/change writers | Дедупликация request_id, одна активная команда, start без config, apply revision, offline queued, expiry, lease/result и отказ общей записи |
| 07 | Каталог, Trap/snapshots, очередь Kafka, своё хранилище, clock | Одинаковый retry/конфликт IDs, валидация batch, commit до ack, 10-секундный pending, фильтры/пагинация, история, исходные auth/action поля |
| 08 | Agent identity/runtime 05, команды 06, ingest 07; WSS-клиент на локальном тестовом сервере | Handshake/hello, stale connection, heartbeat, dispatch/result, неизменный retry, invalid message/close codes |
| 09 | Сессия 02, журнал 07, clock; локальный WSS-клиент | replay → ready → live без пропуска, cursor expiry, отзыв сессии, slow consumer, state_version/tombstone |
| 10 | AuthContext, своё хранилище, change writer | Изоляция, фильтры, неизменяемость, безопасные metadata; согласованную запись проверяет вызывающий модуль |
| 11 | Готовая общая сборка | Сценарии A–G; моки не заменяют итоговый demo |

Для HTTP-тестов — httptest и настоящий handler/service с моками соседей. Бизнес-правила сервиса проверяются отдельно от router. Часы/таймеры управляются тестом: не ждать реальные 24 часа и не использовать случайные задержки. Для WSS нужны реальные локальные handshake/frame/close при моках backend-зависимостей; вызов одного метода сервиса не проверяет протокол.

Готовность 08 на backend-моках проверяет backend WSS. Локальный буфер агента, перезапуск Redis, восстановление listeners и журнал результатов проверяются отдельно у агентской реализации; без неё backend-тест не доказывает эти гарантии.

Для каждого потребляемого интерфейса подготовить одинаковые проверки договора: успешный ответ, ожидаемая ошибка, отмена context, чужая организация/устаревшая версия, где применимо. Прогнать их против fake и затем реального адаптера. Не ограничиваться assert «метод вызван»: проверять возвращаемое состояние, HTTP/WSS-ответ и отсутствие side effect при отказе.

## Атомарные операции: где моки недостаточны

Нельзя соединить модульные методы последовательными независимыми commit и считать инвариант обеспеченным.

| Операция | Одна граница фиксации / синхронизации | Проверка на настоящем хранилище |
|---|---|---|
| Регистрация 02 | Пользователь + организация/членство + код + сессия + audit/уведомления | Параллельный email создаёт одного пользователя без лишней организации |
| Привязка Trap 05 / DELETE Profile 04 | Профиль и активная привязка проверены под общей синхронизацией | Удаление и привязка не оставляют висячий profile_id |
| Create Command 06 / DELETE Trap 05 | Одна блокировка Trap, проверка tombstone/active_command/ingestion, запись результата | Start и DELETE не проходят одновременно |
| RecordResult 06 | Command + Trap runtime/applied/state_version + уведомления | Ошибка commit не отправляет command.ack и не оставляет половину результата |
| Ingest 07 / DELETE Trap 05 | Принятый pending batch учтён до разрешения DELETE; вся пачка Event + уведомления фиксируется согласованно | DELETE не отрезает уже принятый batch; commit до ack; рестарт/повтор без дубликатов |
| Любая обязательная audit-запись | Изменение + audit + уведомление, затем HTTP-успех | Недоступность записи audit откатывает изменение |

При PostgreSQL сервис открывает транзакцию, а участвующие repository работают через один её executor; бизнес-логика остаётся в сервисе, SQL — в repository. Порядок захвата связанных ресурсов команда фиксирует одинаково для конкурирующих операций. DB constraints дополняют service-проверки. Для Kafka приём/pending/materialization согласуются с durable-состоянием в БД; память одного WSS не заменяет учёт ingestion. Технические тесты БД можно писать и запускать отдельно от соседних сервисов на согласованных миграциях.

## Минимальный договор для передачи участнику

Вместе с модулем передаются:

1. Потребляемые интерфейсы с точными Go-сигнатурами и небольшой fake для каждого; только необходимые методы из таблицы.
2. Реальные service/handler/repository модуля и способ их собрать с переданными зависимостями.
3. Тесты success/error/authorization и проверки договора интерфейса; перечень ещё не проверенных внешних гарантий.
4. Миграции собственных таблиц, constraints и индексы, если PostgreSQL нужен; их согласованный порядок в общей migrations/.
5. Список atomic-операций, требующих общей транзакции с соседями, и DTO, которые адаптер переводит между пакетами.

Временный fake располагается в тестовом окружении. Общая сборка не должна незаметно запускаться с fake для авторизации, audit, сохранения событий или подтверждения команд.

## Как объединять готовые модули

1. В internal/app явно создать реальные зависимости. Main остаётся тонким: config, запуск, shutdown. Каждый владелец модуля подключает свои маршруты к общему Gin router, не поднимает отдельный production HTTP-сервер.
2. Соединить 01+02, затем 03+04+05. Подключить реальные audit/change writers до проверки успешных записей; наличие runtime 08/09 для фиксации audit/журнала не требуется.
3. Подключить 06 к snapshots/Trap; проверить apply_config/start/stop как сохранённые команды и гонку start/DELETE.
4. Подключить 07 к PostgreSQL/Kafka и durable-журналу, затем 08 к 05/06/07. Проверить commit до ack, потерю result/ack и reconnect.
5. Подключить 09 к сессиям/журналу и фронтенду. После event.created читать детали GET Event, включая username/password/input поддерживающего типа.
6. Проверить 10 через реальные вызывающие операции, затем пройти [11](11-happy-path.md) на общей сборке, включая stop → досылка → DELETE → чтение истории.

Номера задают удобный порядок соединения, а не запрет писать следующий модуль заранее. 10 и хранение журнала 07 можно готовить параллельно первым: ранние модули всё равно используют их согласованные интерфейсы и моки.

Перед сдачей реализации из `src/backend`: go fmt ./..., go vet ./..., go test ./..., go build ./.... После изменений shared state/горутин — go test -race ./.... SQL-транзакции/constraints проверяются на PostgreSQL, изменённые миграции — на свежей БД. Прохождение тестов с моками отмечается отдельно от прохождения общей интеграции и demo.
