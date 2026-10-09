# 05. Ловушки, жизненный цикл и реквизиты агента

## Передача модуля

- Нужно: 01–04; для безопасного DELETE подключить active_command из 06 и ingestion из 07.
- Отдаёт: Trap/AgentStatus/RuntimeError, token привязки, state_version и tombstone.
- Приёмка: Viewer/чужой id; PATCH между heartbeat; start/DELETE; буфер перед DELETE.
- Общий порядок и границы: [README](README.md).

## Схемы ловушки

| Схема | Поля |
|---|---|
| CreateTrapRequest | `request_id: ID`, `name: Name`, `description?: Description` (default ""), `profile_id: ID` |
| PatchTrapRequest | `name?: Name`, `description?: Description` |
| Trap | `id: ID`, `name: Name`, `description: Description`, `profile_id: ID`, `type_id: TypeID`, `type_version: TypeVersion`, `interaction_level: string`, `revision: Revision`, `state_version: Revision`, `created_at: Timestamp`, `updated_at: Timestamp`, `connectivity: "online" | "offline"`, `last_seen_at: Timestamp|null`, `runtime_state: "unknown" | "running" | "stopped" | "error"`, `desired_state: "running" | "stopped"`, `desired_profile_revision: Revision|null`, `applied_profile_revision: Revision|null`, `agent: AgentStatus|null`, `active_command_id: ID|null` |
| AgentStatus | `agent_version: string` (1..64), `hostname: string` (1..255), `buffered_events: integer` (>=0), `buffer_bytes: integer` (>=0), `buffer_capacity_bytes: integer` (>0), `buffer_state: "ok" | "full" | "unavailable"`, `last_error: RuntimeError|null` |
| RuntimeError | `code: string` (1..64), `message: string` (1..200, безопасный текст) |
| AgentCredentials | `trap_id: ID`, `token: string` (opaque, один показ), `generation: Revision`, `agent_ws_url: string` (wss URL), `issued_at: Timestamp` |

Новая ловушка revision=1, state_version=1, offline, last_seen_at=null, runtime_state=unknown, desired_state=stopped, desired/applied_profile_revision=null, agent=null, active_command_id=null. Регистрация ловушки сама по себе не запускает сервис и не применяет профиль. Для первого запуска используются apply_config и затем start.

online: есть активное прошедшее hello WSS и не более 30 секунд с последнего принятого heartbeat/hello по серверному времени. Разрыв WSS сразу означает offline. Трафик телеметрии не заменяет heartbeat. last_seen_at — последний принятый hello/heartbeat, а не часы агента. offline не означает stopped: runtime_state — последнее подтверждённое наблюдение. Ловушка без агента имеет unknown.

revision увеличивается только при изменении name/description; updated_at отражает последнее такое изменение. Пустой PATCH — 422 validation_failed, неизменённые значения возвращают прежнюю revision. Heartbeat, connectivity, реквизиты и команды не меняют revision/updated_at. PATCH/DELETE требуют X-Expected-Revision и не конфликтуют с heartbeat.

state_version увеличивается при любом изменении полного Trap DTO, включая PATCH, last_seen_at, connectivity, agent, desired/applied и active_command_id. Frontend сравнивает state_version при синхронизации статусов. Версии увеличиваются атомарно с изменением; переполнение применимой версии — 409 revision_exhausted без частичной записи. ETag Trap не выдаётся.

## Маршруты ловушек

| Метод и маршрут | Доступ | Вход | Успех |
|---|---|---|---|
| POST /api/traps | admin, CSRF | CreateTrapRequest | 201 Trap, Location `/api/traps/{id}` |
| GET /api/traps | admin/viewer | limit/cursor, `profile_id?`, `connectivity?` (online/offline) | 200 Page<Trap>, created_at DESC/id DESC |
| GET /api/traps/{id} | admin/viewer | ID | 200 Trap |
| PATCH /api/traps/{id} | admin, CSRF, X-Expected-Revision | PatchTrapRequest, хотя бы одно поле | 200 Trap |
| DELETE /api/traps/{id} | admin, CSRF, X-Expected-Revision | Без тела; условия безопасного удаления ниже | 204 |

profile_id должен существовать в своей организации; чужой/отсутствующий — 404. Тип должен быть available_for_new_profiles, иначе `409 trap_type_unavailable`. profile_id, type и статусы не меняются PATCH; нельзя подменить профиль после появления истории. Для нового типа регистрируется новая ловушка с новым профилем. Сервер не подключается к клиентскому hostname и не разворачивает агент автоматически.

Повтор создания возвращает первоначальный Trap со `Idempotency-Replayed: true`; актуальное состояние читается GET. Секрет агента не выдаётся при создании Trap. Смена привязки и массовые операции не определены. Остановленная ловушка и её история остаются доступными; stop/start определены в модуле 06.

## POST /api/traps/{id}/agent-credentials

Admin, CSRF; обязательное тело `{ "expected_generation": 0 }` для первой выдачи; дальше expected_generation = последней выданной generation. `200 AgentCredentials`. generation начинается с 1. Это выдача/замена реквизитов одного агента, не отдельная сессия оператора. Секрет привязан к одной ловушке, не имеет срока истечения до замены/отзыва, не даёт REST-доступа оператора. `expected_generation` целое 0..2147483646.

Предыдущий token немедленно отзывается, предыдущий WSS закрывается 4401. Потеряв ответ, повтор старого expected_generation получает `409 agent_credentials_changed` (`Agent credentials have changed`); admin читает текущую generation через GET ниже и выполняет новую выдачу. Токен не восстанавливается из GET, не пишется в аудит/WSS фронтенда. Замена не удаляет неподтверждённые события из Redis; новый token той же ловушки отправляет их с прежними event_id.

## GET /api/traps/{id}/agent-credentials

Admin. `200` тело `{trap_id: ID, generation: integer >= 0, active: boolean, issued_at: Timestamp|null}`. generation=0, active=false, issued_at=null до первой выдачи. token отсутствует.

## DELETE /api/traps/{id}/agent-credentials

Admin, CSRF; `If-Match: "agent-credentials:<trap_id>:<generation>"`, ETag выдаётся GET/POST реквизитов. `204`, active=false, generation увеличивается при фактическом отзыве, WSS закрывается 4401. X-Expected-Revision Trap сюда не подходит. Старые generation не принимаются. Повтор отзыва актуальной уже неактивной generation — 204 без нового увеличения. Отзыв не является остановкой сервиса; сначала stop, если нужна остановка. Без credentials агент сохраняет события локально.

## Остановка и возобновление

Остановка: POST /api/traps/{id}/commands с `{request_id: ID, action: "stop", params: {}}`. Возобновление: тот же маршрут с action="start" и новым request_id. Admin и CSRF обязательны. Ответ 201/200 Command означает принятие; UI ждёт succeeded и фактическое runtime_state через GET Trap/trap.changed. Failed/expired не означает успешную остановку/запуск. Offline-команда остаётся queued до подключения, срок 24 часа.

Успешный stop закрывает listeners, завершает открытые взаимодействия с фиксацией заключительных событий и устойчиво сохраняет stopped вместе с результатом команды. Агент и управляющее WSS продолжают работать, heartbeat и досылка продолжаются. Профиль, applied-конфигурация, agent credentials, события и история не удаляются. Start запускает listeners с той же установленной applied revision, не подхватывает новую редакцию Profile. После stop рестарт агента не возобновляет listeners сам: нужна новая команда start. Уже stopped/running — успешный no-op соответствующей команды.

## DELETE /api/traps/{id}

Admin, CSRF, X-Expected-Revision; тело отсутствует. `204` только после безопасного удаления регистрации; процесс/контейнер на хосте этим API не удаляется. DELETE не заменяет stop.

Условия проверяются атомарно с командами и состоянием: нет queued/running команды; активное hello-соединение online; runtime_state=stopped и desired_state=stopped; агент подтвердил buffered_events=0, buffer_bytes=0 и buffer_state=ok; в центре нет незавершённого ingestion этой ловушки. Нулевой буфер включает карантин непринятых событий и сообщается только после ack всех событий, включая закрытие сессий при stop. Offline-удаление отвергается даже при ранее известном stopped.

Исключение: новую ловушку, которой ни разу не выдавались agent credentials и у которой нет активных команд/ingestion, можно удалить без подключения. Выдача credentials и удаление сериализованы; удалённой ловушке credentials не выдаются.

| HTTP | code | message | Условие |
|---|---|---|---|
| 409 | command_in_progress | Another command is in progress | Есть queued/running команда |
| 409 | trap_offline | Trap must be online before deletion | Нет подтверждённого активного агента |
| 409 | trap_not_stopped | Trap must be stopped before deletion | runtime_state/desired_state не stopped |
| 409 | trap_buffer_not_empty | Trap telemetry buffer must be empty before deletion | Буфер не пуст/неисправен либо ingestion не завершён |

После успеха Trap исчезает из списка; GET/PATCH Trap, POST commands и все credentials-маршруты отвечают 404. Token отзывается, WSS закрывается 4401, новый handshake — 401 agent_unauthenticated. Tombstone сохраняет id, организацию, тип, снимки и историю. Профиль больше не считается используемым этой ловушкой. События и команды читаются владельцами организации: GET Event, GET /api/events?trap_id=<удалённый свой id> и оба GET commands. Чужой/неизвестный id — 404. Старый request_id создания даёт 409 request_already_used. Восстановление регистрации не предусмотрено; новая регистрация получает новый id.

Повтор DELETE удалённого id — 404 без второго audit; потерянный ответ проверяется GET Trap (404 для ранее известного своего id). Конкурентные start/DELETE: либо start создаёт active_command и DELETE получает 409, либо DELETE создаёт tombstone и start получает 404. Удаление, отзыв token, audit trap.deleted и notification trap.deleted фиксируются согласованно.
