# 08. WSS агент/ловушка ↔ backend

## Передача модуля

- Нужно: 01–07.
- Отдаёт: Аутентифицированное соединение, runtime, результаты команд и телеметрия для 05/06/07.
- Приёмка: Рестарт после потери result/ack, без отката snapshot; stop/start, буфер и досылка.
- Общий порядок и границы: [README](README.md).

## Handshake и общая оболочка

`GET wss://<management-origin>/assets/stream`, `Authorization: Bearer <agent-token>`, `Sec-WebSocket-Protocol: resource-stream.v1`. Cookie не используется. Токен привязывает соединение к Trap и организации; клиент не назначает organization_id/trap_id сообщением. Credentials не передаются в query, payload или subprotocol. Управляющий канал отдельный от сервисов-приманок и WSS фронтенда.

Успех `101 Switching Protocols`. До upgrade: 401 agent_unauthenticated, 400 invalid_ws_protocol (`Invalid WebSocket subprotocol`), 503 database_unavailable/service_unavailable; тела ErrorResponse. На этом origin cookie operator не даёт доступ; браузерный Origin при agent handshake запрещён — 403 origin_not_allowed. Агент — небраузерный клиент с заголовком Authorization.

Все frames — UTF-8 JSON text, compression отключена, максимум 256 KiB. Binary — close 1003; превышение — close 1009. Ограничение касается каждого frame и итогового сообщения при fragmentation. Envelope: `message_id: ID`, `type: string` (1..64), `reply_to?: ID|null` (на запросах отсутствует/null), `payload: object`. Серверные ответы содержат reply_to ID клиентского запроса, unsolicited-сообщения reply_to=null. message_id нужен для корреляции; долговременная дедупликация — event_id/command_id/batch_id. Неизвестные поля Envelope/схем запросов отвергаются; неизвестный type — Error.

Первое сообщение — agent.hello в течение 5 секунд. До него другие типы запрещены. На ловушку одно активное соединение; новый корректный hello закрывает предыдущее с 4409 session_replaced. Каждый welcome возвращает новый connection_id; старое соединение больше не может менять состояние/подтверждать команды. Отозванный token проверяется и при обработке сообщений, не только при handshake.

## AgentHello и AgentWelcome

`agent.hello` payload = AgentHello:

| Поле | Тип / ограничение |
|---|---|
| boot_id | ID; новый при перезапуске процесса, тот же при reconnect процесса |
| agent_version | string, 1..64 |
| hostname | string, 1..255 |
| supported_types | массив 1..100 `{type_id: TypeID, type_version: TypeVersion, actions: Action[]}`; пары и actions уникальны |
| runtime | AgentRuntime |

AgentRuntime: `runtime_state: "running" | "stopped" | "error" | "unknown"`, `applied_profile_revision: Revision|null`, `buffered_events: integer >=0`, `buffer_bytes: integer >=0`, `buffer_capacity_bytes: integer >0`, `buffer_state: "ok" | "full" | "unavailable"`, `last_error: RuntimeError|null`. applied revision может быть только ранее выданной этой ловушке revision; неизвестная — Error unknown_configuration. null означает отсутствие установленной конфигурации. runtime_state=error требует last_error; остальные допускают last_error=null или диагностическое последнее предупреждение.

`agent.welcome` payload = AgentWelcome: `connection_id: ID`, `trap_id: ID`, `server_time: Timestamp`, `heartbeat_interval_seconds: integer`, `offline_after_seconds: 30`, `max_batch_events: 100`, `max_message_bytes: 262144`, `telemetry_ack_timeout_seconds: 10`, `current_configuration: ConfigurationSnapshot|null`, `desired_profile_revision: Revision|null`, `desired_state: "running" | "stopped"`.

current_configuration — последний успешно подтверждённый центром snapshot, а не команда применения. Welcome никогда автоматически не заменяет установленную локальную конфигурацию. До hello агент восстанавливает локальный snapshot, устойчивое running/stopped и журнал намерений/результатов; они имеют приоритет над более старым current_configuration. Snapshot=2 не откатывается к welcome snapshot=1 после потери command.result/ack. Если локальной конфигурации и незавершённого намерения нет, current_configuration можно восстановить только в stopped, затем сообщить revision heartbeat; запуск требует start. Повреждённый/недоступный журнал означает остановку listeners и runtime error, а не выдуманный успех восстановления. hello сообщает наблюдаемое состояние, но не подтверждает pending-команду: нужен command.result. Если версии типа нет в supported_types, hello отвергается Error unsupported_type, close 4400. Первому агенту без config heartbeat_interval_seconds=10; после применения берётся management из config.

```json
{"message_id":"11111111-1111-4111-8111-111111111111","type":"agent.hello","payload":{"boot_id":"22222222-2222-4222-8222-222222222222","agent_version":"0.1.0","hostname":"decoy-01","supported_types":[{"type_id":"tcp-banner","type_version":1,"actions":["start","stop","apply_config"]}],"runtime":{"runtime_state":"stopped","applied_profile_revision":null,"buffered_events":0,"buffer_bytes":0,"buffer_capacity_bytes":104857600,"buffer_state":"ok","last_error":null}}}
```

## Heartbeat

Агент → `agent.heartbeat`: `{runtime: AgentRuntime}`. Backend → `agent.heartbeat_ack`: `{server_time: Timestamp, heartbeat_interval_seconds: integer, desired_profile_revision: Revision|null, desired_state: "running"|"stopped"}`. reply_to указывает heartbeat. Принятый heartbeat обновляет last_seen и AgentStatus. Не заменяет command.result и не меняет цель команды. Согласованные сведения applied revision могут исправлять наблюдаемое состояние после рестарта; отсутствие локальной конфигурации может изменить applied revision на null. Исходную конфигурацию очередной команды автоматически не применяет.

Управляющие входные сообщения одного активного соединения (hello, heartbeat, progress, result) применяются строго в порядке получения: ранее принятый heartbeat не может позднее затереть результат команды. Telemetry обрабатывается независимо и не меняет runtime/applied state.

Heartbeat идёт каждые 5..10 секунд в зависимости от успешно применённого config. Если 30 секунд нет hello/heartbeat, backend закрывает WSS 4408 heartbeat_timeout и переводит Trap offline. Ping/Pong протокола используется каждые 15 секунд, Pong должен прийти за 10; отсутствие — close 4408. Обработка телеметрии не блокирует heartbeat.

## CommandDispatch / CommandResult / CommandAck

Backend → `command.dispatch`: `command_id: ID`, `lease_id: ID`, `lease_expires_at: Timestamp`, `action: Action`, `params: object`, `configuration: ConfigurationSnapshot|null`, `expires_at: Timestamp`. configuration заполнен только для apply_config, target revision соответствует params. start/stop используют уже установленный snapshot. Выдача переводит queued→running. Lease на 60 секунд; после потери соединения/истечения lease та же команда может выдаваться повторно с новым lease_id. command_id неизменен.

Агент → `command.progress`: `{command_id: ID, lease_id: ID}`. backend → `command.progress_ack`: `{command_id: ID, lease_expires_at: Timestamp}`; продление на 60 секунд, не позже expires_at. Во время долгой операции progress каждые 20 секунд. Неверный lease — Error stale_command_lease.

Агент → `command.result`:

| Поле | Тип |
|---|---|
| command_id | ID |
| lease_id | ID |
| status | succeeded / failed |
| result | object|null; succeeded соответствует result_schema, failed null |
| error | RuntimeError|null; failed непустой, succeeded null |
| runtime | AgentRuntime; фактическое состояние на момент отправки, включая повтор после рестарта |

Backend → `command.ack`: `{command_id: ID, status: "succeeded" | "failed", recorded_at: Timestamp}` только после сохранения результата и актуального состояния Trap из runtime. result описывает итог исполнения; runtime — текущее наблюдение, включая последующую ошибку буфера/запуска. result для tcp-banner: `{runtime_state:"running"|"stopped", applied_profile_revision:Revision|null}`. null допустим только для stop до конфигурации. Для apply_config revision в result и первом succeeded runtime обязана совпасть с target snapshot; start/stop не меняют applied revision. Failed тоже передаёт runtime, включая фактическую revision после отката. Некорректный result/runtime: Error command_result_invalid, статус не считается succeeded.

Агент устойчиво сохраняет намерение с command_id и target snapshot ДО side effect. После исполнения установленный snapshot, running/stopped и terminal result фиксируются согласованно ДО command.result. Повторный dispatch завершённой команды возвращает неизменённые status/result/error с текущим lease_id и свежий runtime без повторного side effect. Crash между применением и terminal commit оставляет незавершённое намерение: после актуального dispatch агент сверяет фактическую конфигурацию и идемпотентно завершает действие в пределах expires_at. До завершения восстановления listeners остановлены, runtime_state=unknown/error. Истёкшее намерение не запускает новых действий; агент сообщает фактическое состояние. Успешный stop устойчиво сохраняет stopped: рестарт его не отменяет. Для будущего action идемпотентное исполнение по command_id обязательно; каталог сам эту гарантию не обеспечивает.

Повтор идентичных terminal status/result/error — повторный ack без новой операции/аудита и без перезаписи Trap из старого result. Свежий runtime обрабатывается как heartbeat; lease_id/runtime не входят в сравнение неизменяемого результата. Другой terminal result — Error command_result_conflict. Для незавершённой команды принимается только текущий lease. Истёкшая команда — Error command_expired и новые side effects не запускаются; backend не утверждает, что ранее выданная команда не успела исполниться: runtime уточняется heartbeat.

## TelemetryBatch / TelemetryAck

Агент → telemetry.batch и backend → telemetry.ack используют [схемы и гарантии приёма из модуля 07](07-events.md). Модуль 08 передаёт закреплённые trap_id/connection_id и payload в модуль 07, а не выполняет второе сохранение. Полный положительный ack отправляется только после PostgreSQL commit. Ошибка/таймаут не удаляют локальный batch; его batch_id/event_id и содержимое сохраняются при retry.

## Буфер Redis и восстановление: внешняя семантика

Redis-буфер находится со стороны агента: события фиксируются локально до отправки в центр, включая работу без сети. Backend Redis может применяться для восстановления служебного состояния, но его отказ не отменяет подтверждённую запись PostgreSQL; Redis и Kafka не доступны оператору/ловушке напрямую через API.

event_id и исходный AgentEvent сохраняются при любом retry/reconnect/рестарте агента. После telemetry.ack удаляются только перечисленные ID; после ошибки ничего не удаляется автоматически. Тихое удаление старых событий/переполнение очереди запрещено. Агент резервирует место для обязательных метаданных сессии до принятия взаимодействия; не может продолжать session без возможности фиксации. buffer_state=full/unavailable — агент прекращает принимать новые взаимодействия, прекращает listeners, runtime_state=error, last_error=buffer_unavailable; уже записанные события сохраняет. При восстановлении сначала досылает данные, возвращается stopped; для запуска нужна команда start. Даже capture_payload=false продолжает запись метаданных подключений.

Положительный локальный commit означает восстановимость после рестарта Redis/агента; без персистентности Redis на стороне агента реализация этому контракту не соответствует. Потеря самого диска/всех копий не объявляется покрытой гарантией. Если события не удалось зафиксировать локально, агент не продолжает обычную работу, будто сбор исправен.

Reconnect: задержка с jitter 1..30 секунд, верхняя граница 30; 4401 останавливает попытки с отозванным token до выдачи нового. После нового welcome отправляются buffered события с прежними версиями и результаты команд по актуальному lease. Отключение браузера никак не влияет на буфер/команды/сбор.

## Ошибки WSS агента

Сервер → `error`: `{error: Error, retry_after_seconds?: integer >=1}`; reply_to содержит message_id запроса. Неизвестный message type или JSON: invalid_message (`Invalid WebSocket message`); неподдерживаемая версия: unsupported_type (`Trap type version is not supported`); остальные коды ниже. Ошибка одного batch не закрывает исправное соединение, если это не auth/протокол/размер.

| code | message | Повтор |
|---|---|---|
| unknown_configuration | Unknown applied configuration | Исправить runtime report |
| stale_command_lease | Command lease is no longer valid | Ждать актуальный dispatch |
| command_expired | Command has expired | Не выполнять |
| command_result_invalid | Command result is invalid | Исправить result |
| command_result_conflict | Command result conflicts with stored result | Не повторять side effect |
| batch_conflict | Batch identifier was used with different content | Не менять старый batch |
| event_id_conflict | Event identifier was used with different content | Не генерировать новый ID для обхода; исправить конфликт вручную |
| telemetry_invalid | Telemetry validation failed | Исправить producer; сохранить непринятые данные |
| ingestion_pending | Telemetry persistence is not yet confirmed | Да, прежние IDs |
| ingestion_busy | Another telemetry batch is in progress | Да, после Retry-After |
| telemetry_unavailable | Telemetry delivery is temporarily unavailable | Да |
| database_unavailable | Database is temporarily unavailable | Да |

fields у telemetry_invalid указывает `/events/<index>/<field>`; никаких исходных payload в message. Коды общих validation_failed/invalid_message применимы к оболочке. Close: 1000 normal, 1003 unsupported data, 1009 too large, 1011 internal, 1013 temporary unavailable, 4400 invalid protocol/hello, 4401 authentication revoked, 4408 timeout, 4409 session_replaced. close reason — только указанный безопасный код, без секретов. Без ack событие всегда остаётся локально независимо от close code.

## Вход событий от ловушки

Ловушка передаёт агенту каждую поддерживаемую попытку входа с введёнными username/password и каждое действие с исходной командой/запросом. Поля определены в модуле 07. Агент не маскирует введённые пароли и не превращает действия атакующего в Command центра. Локальная связь ловушки с агентом не является публичным REST/WSS API, но имеет ту же гарантию: взаимодействие не продолжается без устойчивой фиксации AgentEvent агентом. После локального commit исходные data передаются в telemetry.batch, сохраняются backend и возвращаются frontend как Event.data. capture_payload=false не отключает структурированные auth/action. Буфер резервирует место для этих обязательных событий и завершения сессии; нулевой buffered_events включает также карантин непринятых событий.
