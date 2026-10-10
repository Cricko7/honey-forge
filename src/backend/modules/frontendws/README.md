# Frontend WSS — модуль 09

`app.Open` подключает `GET wss://<operator-origin>/api/stream` к cookie-сессиям
admin/viewer своей организации. Требуются точный `Origin` из `BROWSER_ORIGINS` и
единственный `Sec-WebSocket-Protocol: dashboard-stream.v1`. Compression выключена;
принимаются только text JSON сообщения не более 256 KiB.

После `GET /api/events` отправьте в течение пяти секунд:

```json
{"message_id":"55555555-5555-4555-8555-555555555555","type":"stream.subscribe","payload":{"after":"<EventPage.stream_cursor>"}}
```

`after: null` начинает live с текущей границы. Replay сначала передаёт изменения
до зафиксированной границы, затем `stream.ready` с `reply_to`, затем live.
Записи, зафиксированные во время replay, читаются после ready. Для смены `after`
нужно переподключиться. Опрос PostgreSQL каждые 250 ms обеспечивает доставку
независимо от Redis/Kafka и наличия других браузеров.

Cursor зашифрован и связан с организацией и `dashboard-stream.v1`, но не с
пользователем/сессией. Срок продолжения — 24 часа от выдачи cursor. Некорректный
или чужой cursor: `invalid_cursor`; устаревший: `cursor_expired`. Оба возвращают
коррелированное `error`, затем close 4400; клиент перечитывает REST после expiry.
На первом внедрении миграция сохраняет границу старого журнала без исторических
DTO: для синхронизации используются новые REST/WS cursors. Старые токены прежнего
формата недействительны. Записи журнала пока не удаляются; expiry проверяется
по зашифрованному времени выдачи токена.

Миграция `20261010150000_frontend_stream.sql` записывает безопасные снимки Trap
и Command при INSERT журнала **в бизнес-транзакции**. Event содержит только
summary. Профильный журнал атомарно дополняет общий journal; блокировка организации
берётся до profile/organization locks. Изменение каталога при установке новой
версии атомарно записывает `catalog.changed` для существующих организаций;
повторная установка того же ETag ничего не публикует. Исторические версии
каталога остаются доступны. Существующие `audit.created` профилей доставляются;
расширение полного аудита относится к модулю 10.

У каждого соединения ограничена очередь: 1000 уведомлений / 4 MiB, включая
сообщение в процессе отправки. Превышение закрывает соединение с 4410
`slow_consumer`, без пропуска записей; далее reconnect/replay или новая REST
синхронизация. Медленный браузер не держит транзакции ingestion/command ack.
Ping каждые 15 секунд, Pong за 10 секунд. Сессия/роль проверяются перед каждой
отправкой и ping; logout/expiry/revocation — 4401. Остановка приложения — 1013;
обработчики и их goroutines завершаются до закрытия пула PostgreSQL.

Frontend-кода в репозитории нет. Его обязательный контракт:

- Применять `trap.changed` только при увеличении `state_version`; `revision`
  используется для редактирования. `trap.deleted` устанавливает tombstone,
  защищающий от запоздалых REST/replay ответов.
- Дедуплицировать Event по `event_id`, Command по `id/status`; сохранять cursor
  после локального применения уведомления. Неизвестный type пропускать с
  сохранением cursor. Запись WebSocket не подтверждает применение UI.
- Детали получать через `GET /api/events/{event_id}`: username/password для
  `service.auth_attempt`, input/outcome для `service.action` доступны без
  маскирования. Показывать как текст. Cookie/token/join_code/config secrets
  в уведомления не входят.
- При 4401 проверять `GET /api/session` или вход, без бесконечного reconnect;
  после `cursor_expired` перечитывать актуальные REST ресурсы.

Контракт сообщений и handshake находится в `api/openapi.yaml` (`x-websockets`).
Проверки: `go test ./modules/frontendws ./internal/stream`,
`go test -tags integration ./internal/app -run TestFrontend` с PostgreSQL.
