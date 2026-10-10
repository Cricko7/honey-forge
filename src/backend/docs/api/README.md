# HoneyForge: API-контракт и порядок командной сборки

Статус: спецификация; реализация и инструкции запуска не входят в документ. Номера — порядок сборки и отдельные зоны ответственности. Один участник берёт модуль, другой использует его зафиксированные схемы вместо собственной копии. Нумерация файлов не означает URL-версию API.

Отдельный договор для параллельной разработки: [связывание модулей, моки и независимые проверки](module-integration.md). Он описывает внутренние операции, точки общей фиксации и порядок замены моков реальными зависимостями.

## Порядок работы

| № | Модуль | Нужны | Передаёт следующим участникам | Проверка перед соединением |
|---|---|---|---|---|
| 01 | [Общие правила](01-common.md) | Нет | Ошибки, ID, Page, Envelope, лимиты, preconditions, правила доступа/фиксации | Одинаковые 400/422/413/415; безопасные ошибки |
| 02 | [Операторы и организации](02-auth-organizations.md) | 01 | Cookie, CSRF, AuthContext, организация, роли, join-code | Admin/create, viewer/join, login/logout, изоляция |
| 03 | [Каталог](03-catalog.md) | 01–02 | CatalogEntry, config/event/action schemas, tcp-banner/1 | Чтение обеими ролями, неизвестная версия 404, схемы валидны |
| 04 | [Профили](04-profiles.md) | 01–03 | Profile, revision/ETag, полный внутренний snapshot | CRUD, schema validation, If-Match, серверные секреты скрыты |
| 05 | [Ловушки](05-traps.md) | 01–04 | Trap, revision/state_version, AgentStatus, RuntimeError, credentials, tombstone | Правка не конфликтует с heartbeat; безопасный DELETE |
| 06 | [Команды](06-commands.md) | 01–05 | Command, snapshot, одна активная команда, start/stop/apply_config | request_id, command_in_progress, revision, гонка start/DELETE |
| 07 | [События](07-events.md) | 01–06 | AgentEvent/EventSummary/Event, ingest до ack, REST, stream_cursor и журнал изменений | Дедупликация/конфликт, фильтры, история tombstone, исходные auth/action data |
| 08 | [WSS агента](08-agent-ws.md) | 01–07 | Hello/heartbeat, dispatch/result/ack, telemetry.batch/ack, восстановление | Потеря result/ack, рестарт, досылка, stop/start/DELETE |
| 09 | [WSS фронтенда](09-frontend-ws.md) | 01–08 | Replay/live, event.created, trap.changed/deleted; детали через REST | stream_cursor, state_version, отображение username/password/input |
| 10 | [Аудит](10-audit.md) | 01–09 | AuditEntry, чтение, audit.created, фиксация обязательных действий | Все записи 02/04/05/06 имеют audit без секретов |
| 11 | [Сквозная приёмка](11-happy-path.md) | 01–10 | Демо и проверки соединений | Все сценарии A–G проходят на общей сборке |

02 объединяет авторизацию и организацию: регистрация атомарно создаёт пользователя, организацию/членство, join-code и сессию. 05 владеет ловушкой, 06 — командами. 07 определяет события до реализации транспорта 08; 09 не создаёт вторую схему событий.

## Точки соединения

Это внутренние границы ответственности, не новые HTTP-маршруты. Context отмены передаётся через все вызовы; интерфейсы создаются у потребителя только при реальной необходимости.

| Поставщик → потребитель | Вход | Результат / гарантия |
|---|---|---|
| 02 → operator-модули | Cookie + контекст запроса | AuthContext: user_id, organization_id, role; сессия активна, Origin/CSRF для записи проверены |
| 03 → 04/06/07 | type_id + type_version | Точный CatalogEntry; config/params/result/event.data валидируются одной immutable-версией, не latest |
| 04 → 05 | organization_id + profile_id | Свой профиль/type/version; привязка и DELETE Profile исключают висячую ссылку |
| 04 → 06 | organization_id + profile_id + revision | ConfigurationSnapshot с полным config; иначе profile_changed; snapshot закреплён при создании Command |
| 05 ↔ 06 | trap_id + действие/статус команды | Атомарная проверка tombstone/active_command и запись desired/runtime/state_version; DELETE/start не проходят вместе |
| 05 → 08 | Token, затем connection_id | Закреплённые trap_id/organization_id/type/version; старый token/connection не меняет состояние |
| 06 ↔ 08 | command_id/lease_id, result + runtime | Актуальный dispatch; ack после commit Command/Trap; повторы не запускают side effect |
| 07 ↔ 08 | Закреплённое trap_id, batch_id, AgentEvent[] | Вся пачка сохранена/дедуплицирована в PostgreSQL до telemetry.ack; Kafka ack недостаточен |
| Записывающие модули → 07/09 | Изменение + безопасные data уведомления | Устойчивый журнал изменений; EventPage.stream_cursor согласован с REST snapshot |
| 07 → 09/frontend | stream_cursor / event_id | Replay и GET Event с полным data; захваченные значения не маскируются API |
| 02/04/05/06 → 10 | Actor, action, resource, AuditDetails | Audit согласован с действием, idempotent replay не дублирует запись |

ConfigurationSnapshot определён в 06, но его shape зафиксирован до реализации 04: profile_id, profile_revision, type_id, type_version, config. Публичный Profile с вырезанными writeOnly-полями не заменяет полный snapshot. Единственные владельцы схем: Envelope — 01, CatalogEntry — 03, Profile — 04, Trap — 05, Command/ConfigurationSnapshot — 06, AgentEvent/Event — 07, AuditEntry — 10. AgentRuntime — 08; он использует RuntimeError из 05.

## Параллельная работа и сдача

1. Согласовать 01 и таблицу границ до разделения задач. Каждый владелец проверяет успех, ожидаемую ошибку и отказ viewer/чужой организации, где применимо.
2. Пока соседний модуль не готов, изолированные тесты используют маленький fake на потребляемой границе. Production API не получает временных маршрутов/DTO.
3. Соединять по номерам, проверять последнюю колонку таблицы и границы из 11. Реальная авторизация подключена до защищённых HTTP-проверок; один handshake не означает готовность WSS.
4. Обратные зависимости согласовать заранее: 04 проверяет привязки 05; DELETE в 05 проверяет команды 06/ingestion 07; записи требуют audit 10 и уведомления 09. Общий журнал изменений готов в 07 до WSS 09. Изолированная готовность модуля и готовность сквозного сценария различаются; обязательные зависимости подключаются до финальной сдачи.
5. Frontend идёт по 11: session → catalog → profile → trap → credentials/agent → apply_config/start → events/stream. Для stop/start использует команды; для удаления — stop, досылка, GET revision, DELETE.

## Границы версии

Cookie-сессии; одна организация на пользователя; admin при создании, viewer при вступлении; каталог; runtime Low TCP banner и Medium Redis; явное применение профиля; два WSS; Kafka центра; файловый журнал агента.

Передача логинов/паролей и действий ловушка → агент → backend → frontend описана для поддерживающих сервисов. Medium Redis фиксирует попытки входа и команды; MASK-3/4, изоляция, персистентность буфера и near-real-time проверяются на стенде. Это не утверждение о готовой реализации/полном соответствии PDF. Публичный HTTP-контракт также ведётся в `api/openapi.yaml`.
