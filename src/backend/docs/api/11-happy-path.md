# 11. Последовательности happy path и проверяемые результаты

## Передача модуля

- Нужно: 01–10.
- Отдаёт: Сквозной сценарий и проверка всех соединений.
- Приёмка: Сценарии A–G; success/error/authorization на каждой применимой границе.
- Общий порядок и границы: [README](README.md).

Это последовательности вызовов и наблюдаемые ответы, не инструкции развёртывания.

## A. Организация и роли

1. POST /api/registrations с organization.mode=create → 201 SessionView, role=admin, cookie.
2. GET /api/organization/join-code → 200 JoinCode.
3. Другой браузер: POST /api/registrations с mode=join и действующим code → 201 SessionView, role=viewer.
4. Viewer GET /api/traps и /api/events → 200; POST /api/profiles → 403 forbidden без побочного эффекта.
5. Admin ротирует code → 200; регистрация новым email со старым code → 422 invalid_join_code; существующий viewer сохраняет доступ.

## B. Каталог → профиль → ловушка → агент

1. GET /api/trap-types → Page с tcp-banner/1, config/event/action schemas и UIHints.
2. Frontend строит форму, POST /api/profiles с полным config → 201 Profile, revision=1.
3. POST /api/traps с profile_id → 201 Trap, offline/unknown, applied=null.
4. POST /api/traps/{id}/agent-credentials expected_generation=0 → token, generation=1, agent_ws_url.
5. Агент WSS /assets/stream с Bearer и resource-stream.v1 → 101; agent.hello → agent.welcome.
6. POST /api/traps/{id}/commands action=apply_config, params.profile_revision=1 → 201 queued Command.
7. Агент получает command.dispatch c snapshot → применяет → command.result succeeded с текущим runtime → command.ack. Trap.applied_profile_revision=1.
8. POST команды start с новым request_id → 201; dispatch/result/ack → Trap.running.
9. Viewer GET /api/traps/{id} видит online/running и last_seen; agent credentials GET → 403.

## C. Атака → Kafka → сохранение → WSS dashboard

1. GET /api/events → EventPage со stream_cursor; frontend WSS /api/stream с cookie/dashboard-stream.v1 → subscribe(after=stream_cursor) → replay/ready/live.
2. На TCP listener возникает подключение; агент фиксирует tcp.connection_opened в Redis с event_id, session_id и исходным timestamp.
3. Агент telemetry.batch → события проходят Kafka и сохраняются в PostgreSQL → telemetry.ack с теми же event_id.
4. Только после ack агент удаляет подтверждённые записи локально; frontend получает event.created с EventSummary и cursor.
5. GET /api/events/{event_id} → Event; фильтр trap_id/time/source_ip/event_type возвращает то же событие.
6. Потеря frontend-соединения не влияет на сбор; reconnect after последнего обработанного cursor воспроизводит пропущенные уведомления.

## D. Изменение профиля и команды

1. GET Profile → revision=1 и ETag; PATCH с If-Match → revision=2.
2. Trap desired/applied остаются 1; агент продолжает работать по 1.
3. POST apply_config profile_revision=2 → snapshot версии 2; после успешного result applied=2. Profile revision=3, созданная позднее, в эту команду не попадает.
4. POST stop → queued/running → succeeded/stopped: listeners закрыты, heartbeat/досылка продолжаются. POST start с новым request_id → succeeded/running с прежней applied revision. Offline агент получит команду при reconnect.
5. Занятый порт → Command failed, error.code=port_unavailable, фактическая старая конфигурация сохраняется; HTTP GET Command всё равно 200.

## E. Повтор, потеря связи и отказ зависимости

| Ситуация | Наблюдаемое поведение |
|---|---|
| Потерян telemetry.ack после commit | Повтор тех же batch/event IDs; один Event, повтор ack |
| Kafka/БД недоступны | Error telemetry_unavailable/database_unavailable; ack нет, Redis сохраняет события |
| Сохранение дольше 10 секунд | ingestion_pending; событие может завершить сохранение позже; retry с прежними ID |
| Рестарт агента/Redis | Ранее локально зафиксированные события/результаты восстанавливаются; сохраняются ID |
| Переполнение/отказ локального буфера | buffer_state full/unavailable, listeners останавливаются, runtime error; нет молчаливого удаления |
| Потерян command.ack | Сохранённый result отправляется повторно; команда не выполняется второй раз |
| Lease команды заменён | Старый result не завершает новую lease; журнал результата используется для текущего dispatch |
| Два оператора PATCH одного Profile | Один успех; второй 412 revision_mismatch |
| Повтор POST с тем же request_id | Исходный ресурс, не вторая ловушка/профиль/команда |
| Новый request_id при активной команде | 409 command_in_progress |
| Устаревшая Profile revision в apply_config | 409 profile_changed |
| Пропущен heartbeat или разрыв WSS | offline; actual running/stopped не подменяется предположением |
| Замена token | Старое соединение 4401; старые неподтверждённые события сохраняют ID |
| Истёкшая cookie | HTTP 401, frontend WSS 4401 |
| Устаревший stream cursor | cursor_expired, новая REST-синхронизация, не ложный успешный replay |
| Чужой organization/resource ID | 404 либо invalid_cursor, без чужих данных |
| Удаление используемого Profile | 409 profile_in_use |
| Heartbeat между GET и PATCH Trap | PATCH с прежней revision проходит; state_version меняется независимо |
| Два оператора PATCH одной Trap | Один успех; второй 412 revision_mismatch |
| Потерян result apply_config=2, затем рестарт | Локальный snapshot=2 сохраняется; welcome со snapshot=1 не откатывает его; result досылается по актуальному lease |
| stop, затем start | Порты закрыты после stop; start открывает их с той же applied revision, история сохраняется |
| DELETE offline/running Trap | 409 без удаления и отзыва token; сначала подключение, stop и досылка |
| DELETE при недосланных событиях | 409 trap_buffer_not_empty; сначала telemetry.ack всей очереди |
| DELETE остановленной online Trap после досылки | 204, token отозван, Trap исчезает из списка; события и команды читаются из истории |
| Viewer меняет/останавливает/возобновляет/удаляет Trap | 403 forbidden без побочного эффекта |
| Чужая Trap для stop/start/DELETE | 404 resource_not_found без побочного эффекта |
| Ловушка передала auth attempt/action | Агент сохраняет поля, backend сохраняет Event.data, frontend получает те же значения через GET Event |

## Область обязательных требований

FR-C1..C5: profiles, traps, config/телеметрия, PostgreSQL-read API с фильтрами, cookie/роли/изоляция; креды-приманки требуют поддерживающего типа. FR-A2: передача подключений, auth attempts с логинами/паролями и действий до frontend определена, но tcp-banner не генерирует auth/action. FR-A3/A4: конфигурация/heartbeat, локальный Redis-буфер, повторная доставка. Dashboard: frontend WSS/REST. MASK-1/2: TLS/WSS, нейтральный endpoint; MASK-3/4 проверяются на стенде. Аудит — отдельный read-only модуль.

FR-A1 (два уровня) сознательно не закрывается этой первой версией: по запросу пользователя сначала только Low. Medium добавляется новой записью каталога и runtime, затем использует те же Profile/Trap/Command/Event и оба WSS. High, honeytokens, IoC export, alert rules, автоматическое удалённое развёртывание отсутствуют. Этот документ не утверждает соответствие будущей реализации или наличие протестированного стенда.

## F. Удаление с сохранением истории

1. Admin отправляет stop и ждёт succeeded; агент закрывает listeners и фиксирует завершение текущих сессий.
2. Агент досылает события, получает telemetry.ack, затем сообщает heartbeat stopped с нулевым исправным буфером. Незавершённого ingestion нет.
3. GET Trap → revision; DELETE /api/traps/{id} с X-Expected-Revision и CSRF → 204, trap.deleted, token отозван.
4. GET Trap → 404, список не содержит Trap. GET /api/events?trap_id=<id>, GET Event и GET /api/traps/{id}/commands сохраняют историю.
5. DELETE Profile с If-Match → 204, если других привязок нет; старые Event/snapshots остаются интерпретируемыми.

## G. Проверки соединений команды

Модуль сдаётся после успеха, ожидаемой ошибки и отказа доступа, где применимо. На каждом переходе используется DTO поставщика из README, а не копия с другими полями. Полная сборка проходит A–F. FR-A2 отдельно проверяется на поддерживающем auth/actions сервисе, не вымышленными логинами из TCP-баннера.

| Граница | Сквозная проверка |
|---|---|
| 02 → 03–10 | Сессия своей организации работает; истёкшая/чужой id не раскрывают данные |
| 04 → 05 | profile_id и запрет удаления используемого профиля; после DELETE Trap профиль освобождён |
| 05 → 06 | stop/start и атомарная гонка start/DELETE |
| 06 → 08 | dispatch/result/runtime/ack, рестарт после потери result без отката snapshot |
| 07 → 08 | PostgreSQL commit до ack, повтор batch/event IDs без дубликатов |
| 07 → 09 | REST stream_cursor → replay/live → GET Event с исходными username/password/input |
| 05 → 09 | state_version защищает от старого статуса; tombstone от воскрешения удалённой ловушки |
| 02/04/05/06 → 10 | Обязательный audit без секретов/атакующего payload |
