# Проверки модуля 07

Контракт: [07-events.md](../src/backend/docs/api/07-events.md).
Реализация: [modules/events](../src/backend/modules/events/README.md).

| Требование | Проверка |
|---|---|
| UUID, IP literal, 16 KiB, время +5 минут, sequence | `TestEventEnvelope`, `TestIngestBoundary` |
| Атомарный отказ, конфликт event/batch/session sequence, неизвестный snapshot | `TestEventAtomicConflicts` |
| Канонические IP/time, повтор без изменения received_at/уведомлений | `TestEventAtomicConflicts`, `TestTrapCommandsEventsAndTombstone` |
| Глобальная уникальность ID, гонка организаций, смена credentials | `TestGlobalEventIDConflict`, `TestEventReplayAfterCredentialRotation` |
| Фильтры AND, точные наносекунды, пустая страница, snapshot/stream_cursor | `TestEventSnapshotPagination` |
| Собственная организация, viewer, anonymous, tombstone | `TestStructuredEventHistory`, `TestEventSnapshotPagination`, `TestTrapCommandsEventsAndTombstone` |
| Исходные username/password/input, null/empty, UTF-8 лимиты | `TestStructuredEvents`, `TestStructuredEventHistory` |
| Ранее выданная revision, история после изменения/удаления Profile | `TestStructuredEventHistory` |
| EventSummary без data, журнал без захваченных credentials | `TestEventAtomicConflicts`, `TestStructuredEventHistory` |
| Kafka ACK не заменяет PostgreSQL commit | `TestJournalBeforeMaterialization`, `TestBrokerAckIsNotTelemetryAck` |
| Реальный Kafka protocol, key и JSON record | `TestKafkaJournal` (требуется TEST_KAFKA_BROKERS) |
| NUL в захваченном пароле/input без потери данных | `TestStructuredEventHistory` |
| Неизменяемый batch после сбоя Kafka, snapshot до публикации | `TestEventJournalPinsBatchBeforeBrokerFailure` |
| capture_payload=false и общий лимит TCP-сессии без повторного списания | `TestEventPayloadCaptureBudget` |
| Окончательное отклонение при закрытии соединения освобождает fence | `TestEventRejectedBatchReleasesFenceAfterCancellation` |

Основной entrypoint требует Kafka. Материализация синхронная: при ошибке/разрыве
агент досылает тот же batch; БД обеспечивает единственный эффект. Встроенный
`app.Open` допускает fake publisher/прямую БД для изолированных проверок.

Согласованное дополнение для поддерживающих типов: snapshot перечисляет сервисы
в `config.services[].name`. tcp-banner/1 сохраняет исходную immutable-схему.

Границы: агент с локальным журналом и Low/Medium ловушками реализован;
frontend WSS replay/live и отображение данных относятся к модулю 09.
Сквозная проверка Medium с реальным backend/БД не выполнялась.

## Аудит 2026-10-10

Проверка не подтверждает наличие всего сквозного функционала документа:

| Часть цепочки | Состояние |
|---|---|
| Backend ingest, дедупликация, atomic commit/ACK, REST-история | Реализовано и проверено с PostgreSQL |
| Исходные auth/action данные поддерживающих типов | Реализована общая обработка и рабочий `redis-emulator/1`; протокол и события проверены локально без интеграционного теста |
| Kafka publisher | Реализован; реальные broker protocol/key/record требуют отдельного стенда |
| Самостоятельное восстановление PostgreSQL из Kafka | Consumer отсутствует; восстановление требует retry того же batch агентом |
| Сохранение событий агентом до продолжения взаимодействия, local buffer, quarantine, producer truncation | Агент сохраняет события в файловом журнале до локального ACK; полный стенд для Medium не запускался |
| Доставка frontend event.created, replay/live по stream_cursor | Есть транзакционный журнал, frontend WSS отсутствует |
| Отображение details с безопасным текстовым выводом | Frontend отсутствует |

Исправлены воспроизведённые тестами ошибки: отказ PostgreSQL на допустимом NUL,
смена содержимого batch после неудачной публикации, публикация неизвестного
snapshot, обход capture_payload/лимита сессии и зависший fence при окончательном
отклонении с отменённым контекстом.

Неопределённый исход ingest сохраняет fence до повторной обработки batch.
Автономного reconciler нет: при окончательной потере агента такая резервация
может продолжать блокировать безопасное удаление ловушки. Очистка по таймеру
не выполняется, поскольку она могла бы нарушить сохранность неподтверждённых данных.

Captured data хранится отдельно как PostgreSQL JSON: jsonb не поддерживает NUL.
Откат миграции `20261010130000` при наличии NUL намеренно завершится ошибкой,
а не заменит/потеряет исходные данные.

Результаты проверки: `go fmt ./...`, `go vet ./...`, `go test ./...`,
`go build ./...` и `go test -race -tags=integration ./... -count=1` выполнены
успешно. Интеграционные тесты применили все миграции на чистые схемы настоящего
PostgreSQL. `TestKafkaJournal` пропущен: `TEST_KAFKA_BROKERS` не задан,
Docker/Java на этом стенде недоступны. Проверка Kafka с настоящим брокером
остаётся незавершённой; fake publisher не заменяет её.
