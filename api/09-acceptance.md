# 09. Frontend stream: реализация и проверки

| Требование | Проверка |
|---|---|
| Exact Origin, dashboard-stream.v1, admin/viewer; безопасные HTTP ошибки | `TestFrontendHandshake`, `TestHandshakeAuthenticationFailures` |
| REST stream_cursor → replay → correlated ready → live без окна потерь | `TestFrontendRESTReplayLiveReconnect`, `TestReplayReadyLive` |
| Изоляция организации, другая сессия своей организации, expiry 24h | `TestFrontendInvalidCursorAndSubscribe`, `TestFrontendCursor`, `TestFrontendRESTReplayLiveReconnect` |
| Исторические state_version/DTO, trap tombstone, безопасные поля | `TestFrontendRESTReplayLiveReconnect` |
| Журнал после рестарта, queued/running/succeeded снимки Command | `TestFrontendReplaySurvivesRuntimeRestart`, `TestFrontendCommandReplayKeepsStatusSnapshots` |
| Устойчивое catalog.changed, без повторов одинакового ETag | `TestFrontendCatalogInvalidationIsDurableAndIdempotent` |
| event.created после commit, summary без data, GET деталей | `TestFrontendEventSummaryAfterCommit`, существующий `TestStructuredEventHistory` |
| Лимиты 1000 / 4 MiB, close 4410 без блокировки journal | `TestQueueLimits`, `TestSlowConsumerClosesWithoutBlockingJournal` |
| Logout/проверка перед доставкой и shutdown 1013 | `TestFrontendRevocationAndShutdown` |
| Первое subscribe за 5 секунд | `TestFirstSubscribeTimeout` |
| Ping 15s, сессия на ping, close 4408 при отсутствии Pong за 10s | `TestPingRequiresPongWithinTenSeconds` |
| Text-only, 256 KiB, disabled compression, cancellation | `TestTransport`, `TestReadCancellation` |

Браузерного приложения здесь нет: локальное применение state_version/tombstone,
дедупликация, сохранение cursor, отображение GET деталей и reconnect UI описаны
в контракте для frontend. Сквозная UI-приёмка этим backend не подтверждается.
Единое расширение AuditEntry/audit.created подключается модулем 10; существующие
уведомления аудита профилей уже проходят через общий журнал.
