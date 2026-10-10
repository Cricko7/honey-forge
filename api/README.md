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
| 07 | [События](../src/backend/docs/api/07-events.md) | Подключена необходимая для 05 граница: PostgreSQL ingestion/ACK, REST, дедупликация и история tombstone; отдельная Kafka-материализация не вводилась |
| 08 | [WSS агента](../src/backend/docs/api/08-agent-ws.md) | Backend hello/heartbeat, dispatch/result, telemetry/ack подключён; отдельный агент/Redis-буфер вне backend |
| 09 | [WSS фронтенда](../src/backend/docs/api/09-frontend-ws.md) | Есть общий транспорт и хранилища изменений; replay/live и доставка уведомлений ещё не подключены |
| 10 | [Аудит](../src/backend/docs/api/10-audit.md) | Запись audit выполняется в транзакциях 02/04/05/06; единое чтение AuditEntry и audit.created ещё не подключены |
| 11 | [Сквозная приёмка](../src/backend/docs/api/11-happy-path.md) | Проверена связка session → catalog → profile; полные сценарии A–G требуют 05–10 и агента/frontend |

## Подключение существующих модулей

Общая сборка находится в `internal/app`: `Open` создаёт зависимости,
`RegisterServices` регистрирует 02 → 03 → 04 на одном Gin router.
`ProfileTypeLookup` передаёт профилям точную схему каталога. Основной entrypoint —
`cmd/api`; совместимый entrypoint за HTTPS-прокси использует тот же `Open`.

DTO принадлежат своим модулям. Перемещение пакетов и реализация следующих
модулей не требуются для соблюдения последовательности подключения.
Общие внутренние границы описаны в
[договоре интеграции](../src/backend/docs/api/module-integration.md).

Важные незавершённые соединения: привязки Trap/Profile, единое чтение и доставка
журналов `auth_changes`, `profile_changes` и общего журнала изменений, runtime
агента и frontend. Наличие общего WSS-транспорта не означает готовность 08/09.

## Контракт и проверки

Markdown описывает продуктовый план. [OpenAPI](openapi.yaml) фиксирует
реализованные HTTP-маршруты согласно правилам репозитория; тест
`TestOpenAPIContainsEveryRouteAndResponseReference` сравнивает его с router.
Согласованное дополнение: назначение и очистка одного writeOnly-поля в одном
запросе возвращают **422 validation_failed**, без изменения данных.

Копия исходного плана в `src/backend/docs/api` сверена с переданной папкой.
Прогресс реализации отслеживается здесь, отдельно от исходной спецификации.
Команды запуска и проверок находятся в [README проекта](../README.md).
