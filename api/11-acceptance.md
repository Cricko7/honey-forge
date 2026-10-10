# Сквозная приёмка 11

Требования: [11-happy-path.md](../src/backend/docs/api/11-happy-path.md).
Публичный HTTP-контракт: [OpenAPI](openapi.yaml).

В общей сборке подключены организация/сессия, каталог, профили, ловушки,
команды, Kafka ingestion, события, оба WSS и чтение аудита. Агент использует
локальный Redis с AOF, fsync-always и noeviction. Новые миграции применяются
Goose при запуске API.

| Сценарий | Проверки |
|---|---|
| A: организация и роли | `internal/app/operator*_integration_test.go`, `audit_integration_test.go`; создание/join, rotation, cookie/CSRF, viewer и изоляция |
| B: каталог → профиль → ловушка → агент | `TestDemoRegistrationToTCPStop`, `TestTrapCommandsWSSLifecycle`; настоящий агент, TLS/WSS, apply_config/start/result/ack |
| C: TCP → Redis → Kafka → PostgreSQL → dashboard | `TestDemoRegistrationToTCPStop`, `TestDemoRegistrationToTCPStopAfterWSSLoss`; live EventSummary, REST detail, cursor replay без повторения обработанного события |
| D: конфигурация и stop/start | Общий demo применяет revision=2 и проверяет закрытие/возобновление порта; `TestApplyConfigRollsBackWhenNewListenerCannotStart` проверяет port_unavailable и старую конфигурацию |
| E: повторы, отказ и восстановление | `TestRedisServerCrashRestoresDurableEventsAndResults`, `TestRedisJournalDurabilityIdentityAndLease`, `TestRestartRestoresSnapshotRuntimeAndReplaysResult`, `TestDemoTrapToBackendAfterLostCommandAck`, `TestBufferFullStopsListenersRetainsEventsAndRequiresStart`; остальные batch/lease/concurrency/cursor проверки находятся в `internal/app` |
| F: удаление с историей | Общий demo ждёт stopped/пустой буфер, удаляет ловушку, читает команды/события и удаляет освобождённый профиль; `TestConcurrentStartAndDelete`, `TestTrapDeleteWaitsForIngestionReservation` проверяют гонки |
| G: соединения и audit | `TestAuditUnifiedHistoryReplayAccessAndStream`, `TestAuditPaginationExcludesLateCommitWithEarlierTimestamp`, существующие тесты доступа/отказа транзакций по модулям; уведомления содержат только audit_id |

## Запуск

Из `src/backend`, с отдельной тестовой PostgreSQL базой:

```powershell
$env:TEST_DATABASE_URL = '<test PostgreSQL DSN>'
$env:TEST_KAFKA_BROKERS = '127.0.0.1:29092'
$env:TEST_REDIS_URL = 'redis://127.0.0.1:6379/0'
$env:TEST_REDIS_SERVER_PATH = '<absolute path to redis-server executable>'
$env:GIN_MODE = 'release'
go test -tags=integration ./... -count=1
go test -race -tags=integration ./... -count=1
```

Для `-race` на Windows нужны C-компилятор и `CGO_ENABLED=1`.
Тест рестарта Redis запускает отдельный экземпляр на случайном loopback-порту,
аварийно завершает его и восстанавливает из AOF в тестовом каталоге. Он не
перезапускает Redis, указанный в `TEST_REDIS_URL`. Без переменных зависимости
соответствующие интеграционные проверки пропускаются: это не полный успех demo.

FR-A2 проверяется существующими `events_structured_integration_test.go` на
поддерживающем типе и отдельным runtime redis-emulator. TCP banner не создаёт
вымышленные auth/action события. Добавление новых уровней взаимодействия и
фронтенд-приложения не требуется для проверки существующего REST/WSS договора.

Локальные TLS/WSS-тесты не подтверждают MASK-3/4 на внешнем стенде. Проверки
сетевой маскировки и окружения выполняются отдельно при развёртывании.
Для старых mutation_audit доступны только email/role на момент миграции;
будущие записи сохраняют неизменяемый снимок автора при самой операции.
