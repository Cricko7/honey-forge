# Проверка общего модуля 01

Каждое общее правило сопоставлено с кодом и проверкой. Это передача общих
механизмов; feature DTO/маршруты и настоящая сессия подключаются их владельцами.

| Требование 01 | Реализация | Проверка |
|---|---|---|
| HTTPS, shutdown, JSON slog | cmd/api/main.go | build; WSS/HTTP httptest TLS |
| Пути WSS и фиксированный HTTPS endpoint агента, без токенов URL | stream/socket.go, endpoint.go | TestAgentEndpoint, TestFailedHandshake |
| JSON media type, 256 KiB до binding на разных маршрутах | contract/body.go, app/protected.go | TestBinding, TestRESTBodyWithoutBinding, TestProtectedRouteOrder |
| Единственный документ, UTF-8, неизвестные/повторные ключи, неверные типы | contract/http.go, json_shape.go | TestBinding, TestNestedBinding |
| 400/422; presence и nullable | contract/presence.go, nullable.go, domain_types.go | TestDomainTypes, TestRequiredNullable, TestRevisionBinding |
| UUID path/body, Name, Description, type/action/event IDs, revision/type_version, RFC3339 UTC, padded bytes | contract/types.go, domain_types.go, preconditions.go | TestDomainTypes, TestSharedTypes, TestRevisionBinding |
| Page items=[] и next_cursor=null | contract/types.go | TestSharedTypes, TestZeroPage |
| X-Request-ID, единые безопасные errors, 20 fields, Retry-After, no-store | contract/errors.go, http.go | TestBinding, TestSafeErrorFields, TestRetryHeaders, TestConcurrentRequests, TestRouterErrors |
| GET не меняет бизнес-состояние | contract/request_context.go, mutation/store.go | TestGETCannotWriteBusinessState |
| 201 Location, replay 200, пустые 204, Profile ETag, catalog cache/ETag | contract/rest.go | TestResponseRules, TestPreconditions |
| Серверная организация, роли admin/viewer/agent и отсутствие чужих данных | contract/access.go, browser.go | TestAccess, TestPermissions, TestOrganizationIsolation, TestAtomicFailureAndAccess, TestAgentChanges |
| Authentication → Origin/CSRF → role → ownership → input; write до отказа невозможен | app/protected.go, contract/browser.go, mutation/store.go | TestProtectedRouteOrder, TestOwnershipBeforeBody, TestBrowserGuard, TestAtomicFailureAndAccess |
| Draft 2020-12, local refs, безопасные errors, immutable version | configschema/schema.go, repository.go; migration schema_versions | TestSchema, TestSafeSchemaErrors, TestImmutableVersionsAcrossRestart |
| writeOnly: redact/set pointers/preserve/clear; эффективный config 128 KiB | configschema/secrets.go, secret_walk.go, json_pointer.go | TestSecrets, TestRecursiveSecretRedaction, TestEffectiveConfigLimit |
| Назначение и очистка одного поля: 422 (согласованное правило) | configschema/secrets.go | TestSecretAssignmentAndClearConflict |
| If-Match / X-Expected-Revision, 428/400/412, revision exhaustion, atomic update | contract/preconditions.go, mutation/journal.go | TestPreconditions, TestTrapPreconditions, TestAtomicRevision |
| Heartbeat/agent change не увеличивает editable revision | mutation/agent.go | TestAgentChanges (SQL revision/state_version) |
| Пожизненные ключи, scope organization/route/trap, replay без side effect/audit, conflict/deleted | mutation/store.go; mutation_requests | TestDurableIdempotency, TestConcurrentCreate |
| Порядок JSON keys и числовые эквиваленты не влияют на fingerprint | mutation/fingerprint.go, number.go | TestDurableIdempotency, TestNormalizedNumbers |
| limit/default, query rejection, time range, scoped cursor и first-page boundary | contract/query.go, cursor.go; mutation/journal.go | TestQuery, TestTimeRange, TestCursor, TestJournalReplayAndSnapshot |
| Бизнес-запись + обязательный audit + воспроизводимый change journal атомарны | mutation/store.go, record.go; PostgreSQL migrations | TestAtomicFailureAndAccess, TestNotificationWriteFailureRollsBackAudit, TestDurableIdempotency, TestJournalReplayAndSnapshot |
| REST snapshot и stream boundary согласованы | mutation/journal.go | TestJournalReplayAndSnapshot |
| WSS Envelope / request reply / notification, no compression, text-only, 256 KiB, context cancellation | contract/envelope.go, stream/socket.go | TestEnvelope, TestEnvelopeNotification, TestTransport, TestFailedHandshake, TestReadCancellation |
| Goose на свежем хранилище; одновременные миграции | postgres/postgres.go | TestFreshConcurrentMigrations |
| DSN/credentials не попадают в стандартный текст ошибки подключения | postgres/errors.go | TestConnectionErrorsDoNotExposeSecrets |

## Границы, прямо заданные исходным файлом

- Настоящие cookies/sessions/CSRF secrets и создание организации предоставляет 02.
  Общий Guard уже реализован; тестовая подстановка Principal не входит в production.
- Catalogue entry/schema set и runtime реализации типов предоставляет 03/агент.
  Здесь готов валидатор и PostgreSQL-механизм неизменяемой версии схемы.
- Profile/Trap/Command ресурсы и их lifecycle/SQL принадлежат 04/05/06.
  Координатор, заголовки, revision, идемпотентность, audit/change transaction готовы;
  их callbacks обязаны проверять ownership/lifecycle внутри того же Tx.
- Event DTO, occurred_at/received_at, batch/event/command дедупликация и frontend
  rendering принадлежат 07–09. Общий транспорт ничего не исполняет из payload,
  не переписывает timestamps и не маскирует Event.data как config.
- Журнал уже сохраняется устойчиво; конкретное чтение/доставка notifications и
  публичный AuditEntry подключаются 07/09/10. Эти модули используют существующую
  фиксацию, а не выполняют бизнес-операцию повторно.

Сквозная продуктовая сборка, агенты и UI требуют последующих модулей и проверок 11.
