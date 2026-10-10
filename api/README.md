# Порядок сборки и состояние модулей

Порядок взят из [исходного плана](../src/backend/docs/api/README.md).
Номера обозначают зависимости и очередность подключения, а не версии URL или
порядок SQL-миграций. Миграции выполняет Goose по timestamp; опубликованные
миграции не перенумеровываются.

| № | Спецификация | Реализация и состояние |
|---|---|---|
| 01 | [Общие правила](01-common.md) | `internal/contract`, `configschema`, `mutation`, `postgres`, `stream`; [проверки](01-acceptance.md) |
| 02 | [Операторы и организации](../src/backend/docs/api/02-auth-organizations.md) | `src/backend/modules/auth`; реальные cookie-сессии и PostgreSQL подключены |
| 03 | [Каталог](03-catalog.md) | `internal/catalog`; точные immutable-версии, схемы и авторизованное чтение; [проверки](03-acceptance.md) |
| 04 | [Профили](../src/backend/docs/api/04-profiles.md) | `src/backend/modules/profiles`; CRUD, каталог, revision/ETag и snapshots подключены; [границы](../src/backend/modules/profiles/README.md) |
| 05 | [Ловушки](../src/backend/docs/api/05-traps.md) | Подключён: CRUD, heartbeat/state_version, generation/token, безопасный DELETE и tombstone |
| 06 | [Команды](../src/backend/docs/api/06-commands.md) | Подключён к TrapBoundary: apply/start/stop, snapshots, expiry, lease/result и история |
| 07 | [События](../src/backend/docs/api/07-events.md) | Kafka-журнал → атомарный PostgreSQL ingestion/ACK, REST/snapshot cursors, дедупликация, tombstone и структурированные auth/action для поддерживающих типов; [проверки и границы](07-acceptance.md) |
| 08 | [WSS агента](../src/backend/docs/api/08-agent-ws.md) | Backend и cmd/agent подключены: hello/heartbeat, dispatch/result/ack, telemetry/ack, локальный Redis, восстановление snapshot/runtime и очереди |
| 09 | [WSS фронтенда](../src/backend/docs/api/09-frontend-ws.md) | `/api/stream`, replay/ready/live, безопасные DTO, session checks, backpressure; [проверки и границы](09-acceptance.md) |
| 10 | [Аудит](../src/backend/docs/api/10-audit.md) | Подключён: единое чтение AuditEntry, фильтры/курсор, снимок автора и audit.created в общем журнале; записи атомарны с 02/04/05/06 |
| 11 | [Сквозная приёмка](../src/backend/docs/api/11-happy-path.md) | Сквозной тест registration → profile → trap → Redis-агент → TCP → Kafka → PostgreSQL → dashboard replay → revision 2 → stop/start → DELETE/history; [проверки A–G](11-acceptance.md) |

## Подключение существующих модулей

Общая сборка находится в `internal/app`: `Open` создаёт зависимости,
`RegisterServices` регистрирует 02 → 03 → 04 на одном Gin router.
`ProfileTypeLookup` передаёт профилям точную схему каталога. Основной entrypoint —
`cmd/api`; совместимый entrypoint за HTTPS-прокси использует тот же `Open`.

DTO принадлежат своим модулям. Перемещение пакетов и реализация следующих
модулей не требуются для соблюдения последовательности подключения.
Общие внутренние границы описаны в
[договоре интеграции](../src/backend/docs/api/module-integration.md).

Все журналы 02/04/05/06 публикуют audit.created в общий WSS поток. Модуль 10
читает три хранилища через единое PostgreSQL view, не переписывая историю при
изменении пользователя. Для старых mutation_audit миграция может восстановить
только текущие email/role на момент миграции: исходные снимки раньше не хранились.

## Контракт и проверки

Markdown описывает продуктовый план. [OpenAPI](openapi.yaml) фиксирует
реализованные HTTP-маршруты согласно правилам репозитория; тест
`TestOpenAPIContainsEveryRouteAndResponseReference` сравнивает его с router.
Согласованное дополнение: назначение и очистка одного writeOnly-поля в одном
запросе возвращают **422 validation_failed**, без изменения данных.

Копия исходного плана в `src/backend/docs/api` сверена с переданной папкой.
Прогресс реализации отслеживается здесь, отдельно от исходной спецификации.
Команды запуска и проверок находятся в [README проекта](../README.md).
