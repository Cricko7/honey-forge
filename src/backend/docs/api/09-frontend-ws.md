# 09. WSS backend → frontend

## Передача модуля

- Нужно: 01–08; audit.created подключается с 10.
- Отдаёт: Replay/live и уведомления для frontend; данные Event остаются в 07.
- Приёмка: REST cursor → subscribe; state_version/tombstone; GET деталей auth/action.
- Общий порядок и границы: [README](README.md).

## WSS frontend handshake

`GET wss://<operator-origin>/api/stream`, `Sec-WebSocket-Protocol: dashboard-stream.v1`, cookie __Host-session. Только admin/viewer своей организации. Точный Origin обязателен; cookie не является достаточной защитой handshake. Успех 101. До upgrade ошибки: 401 unauthenticated, 403 origin_not_allowed, 400 invalid_ws_protocol, 503 database_unavailable/service_unavailable.

Этот WSS передаёт только уведомления и запрос подписки; команды/изменения выполняются REST с CSRF. Envelope определён в модуле 01; EventSummary — в модуле 07. Frames text JSON <=256 KiB; binary 1003, превышение 1009, compression отключена. Payload событий/ловушек/команд не содержит cookies, agent token, join_code и writeOnly secrets.

## StreamSubscribe / StreamReady

Первое сообщение за 5 секунд — `stream.subscribe`, payload `{after: Cursor|null}`. after=null начинает live с текущей границы; after из REST EventPage или ранее полученного WS cursor запускает replay. Подписка охватывает все изменения своей организации: events, traps, commands, catalog, audit. Параметра organization_id нет. Изменение подписки внутри открытого соединения не поддерживается; для нового after используется reconnect.

Backend отвечает `stream.ready`: `{cursor: Cursor, server_time: Timestamp, replayed: boolean}`; reply_to исходного subscribe. При replay сначала выдаются все изменения после after до зафиксированной текущей границы, затем ready этой границы, затем live. Уведомления, появившиеся во время replay, не пропускаются и поступают после ready. cursor нельзя сравнивать лексикографически; он непрозрачен.

Cursor привязан к организации и stream-версии, но не к сессии пользователя; другая сессия той же организации может продолжить. Replay гарантируется 24 часа; более старый cursor — `error` с cursor_expired (`Stream cursor has expired`), close 4400. Некорректный/чужой — invalid_cursor и close 4400. after отсутствует/не соответствует схеме — invalid_message. Вновь подключённый клиент не получает чужие изменения даже при подмене cursor.

```json
{"message_id":"55555555-5555-4555-8555-555555555555","type":"stream.subscribe","payload":{"after":null}}
```

## StreamChange и типы уведомлений

Каждый notification payload = `{cursor: Cursor, occurred_at: Timestamp, data: object}`. occurred_at здесь — серверное время изменения, не время атаки. Backend генерирует message_id; reply_to=null. Cursor продвигается в порядке устойчиво записанных изменений: после восстановления не возникает ранее невидимая запись перед уже выданным cursor.

| Envelope.type | data |
|---|---|
| event.created | `{event: EventSummary}` |
| trap.changed | `{trap: Trap}` — безопасный DTO, revision и state_version |
| trap.deleted | `{trap_id: ID}` — убрать из актуального списка, сохранить доступ к истории |
| command.changed | `{trap_id: ID, command: Command}` |
| profile.changed | `{profile_id: ID, revision: Revision}` |
| profile.deleted | `{profile_id: ID}` |
| catalog.changed | `{etag: string}` — новое значение ETag полного каталога |
| audit.created | `{audit_id: ID}` |

Неизвестный type клиент пропускает, но сохраняет cursor. trap.changed с меньшей/равной state_version не перезаписывает более новую/такую же локальную Trap. revision используется только для редактирования. trap.deleted создаёт локальный tombstone: запоздалый replay/REST-ответ trap.changed не воскрешает этот id. event.created содержит только summary; data запрашивается GET /api/events/{event_id}. Для service.auth_attempt возвращаются username/password, для service.action — input/outcome; UI читает и показывает эти детали, API не маскирует захваченные значения. Старые версии каталога доступны для интерпретации исторических событий. Kafka не является браузерным протоколом; frontend работает только через REST/WSS.

Пример:

```json
{"message_id":"66666666-6666-4666-8666-666666666666","type":"profile.changed","reply_to":null,"payload":{"cursor":"opaque-org-stream-cursor","occurred_at":"2026-10-09T12:00:01Z","data":{"profile_id":"77777777-7777-4777-8777-777777777777","revision":2}}}
```

## Доставка, reconnect и backpressure

event.created выдаётся только после сохранения Event в PostgreSQL; задержка Redis/Kafka не порождает уведомление о несуществующем Event. Журнал уведомлений восстанавливается после рестарта центра; запись бизнес-изменения и его воспроизводимого уведомления не могут разойтись так, чтобы REST содержал Event, а stream навсегда его потерял. Это требование публичной семантики; схема хранения не задаётся здесь.

На одном соединении порядок cursor сохраняется. При reconnect возможны повторы: Event дедуплицируется frontend по event_id, Command по id/status, Trap по id/state_version. Последний обработанный cursor сохраняется только после применения уведомления локально. «Сообщение отправлено в браузер» не означает, что браузер его применил.

Начальная загрузка: GET /api/events → EventPage stream_cursor → subscribe after=stream_cursor → replay → ready → live. Параллельно GET profiles/traps/commands; более новые Trap state_version из REST нельзя затереть старым replay. Для полной синхронизации после cursor_expired frontend перечитывает актуальные REST-ресурсы и начинает с новой границы; утраченный replay не выдаётся как успешный.

Медленный frontend не блокирует сбор и command ack. Максимум 1000 неподанных уведомлений или 4 MiB на соединение; превышение close 4410 slow_consumer, затем reconnect/replay. Никакого молчаливого пропуска. Когда replay снова не успевает, клиент выполняет свежую REST-синхронизацию. При отказе центра — close 1013; данные не исчезают из-за отсутствия браузера.

Ping каждые 15 секунд, Pong за 10 секунд. Сессия/роль проверяются перед выдачей данных и при каждом серверном ping; expiry/logout/revocation закрывает 4401. После 4401 frontend делает GET /api/session или login, не reconnect бесконечно с недействительной cookie. Close также: 1000 normal, 1003, 1009, 1011 internal, 1013 unavailable, 4400 invalid protocol/subscribe, 4403 forbidden, 4408 timeout, 4410 slow_consumer. Close reason — безопасный код.

## WSS Error

Envelope.type=error; payload `{error: Error, retry_after_seconds?: integer >=1}`. До валидной подписки ошибки связаны reply_to с subscribe. Дополнительные frontend codes: cursor_expired, invalid_cursor, invalid_message, service_unavailable; сообщения соответственно `Stream cursor has expired`, `Invalid pagination cursor`, `Invalid WebSocket message`, `Service is temporarily unavailable`. Для invalid_cursor слово pagination унаследовано из общей ошибки, смысл конкретизируется типом сообщения. Клиент выбирает поведение по code.

## Near-real-time

При исправном соединении и доступных зависимостях flush агента <=1 секунды; целевой сценарий — уведомление frontend не позднее 3 секунд после локальной фиксации события. Это цель проверки happy path, а не успешный ack до реального commit. При очереди/отказе возможна большая задержка; время occurred_at сохраняется, received_at отражает фактическую доставку. API не скрывает degraded состояние под ложным подтверждением.
