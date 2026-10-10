# Ловушки: tcp-banner/1 и redis-emulator/1

Агент обслуживает одну регистрацию Trap. По команде `start` он создаёт отдельный
процесс TCP-ловушки из ранее применённой конфигурации. Ловушка подключается к
локальному `ws://127.0.0.1:<случайный порт>/trap-stream`, агент — к существующему
`wss://<backend>/assets/stream`. Локальный WebSocket защищён случайным токеном,
который выдаётся дочернему процессу через stdin pipe вместе со snapshot. На атакующих TCP-портах нет доступа
к управляющему каналу. Дочерний процесс не получает backend-токен агента.

## Создание по запросам с конфигурацией

Используются существующие REST-маршруты. Все POST требуют cookie-сессию admin,
разрешённый `Origin`, `X-CSRF-Token` и `Content-Type: application/json`.
Подставляйте новый UUID в каждый `request_id`.

1. `POST /api/profiles` — создать профиль конфигурации:

```json
{
  "request_id": "<UUID>",
  "name": "Demo TCP",
  "description": "Первая TCP-ловушка",
  "type_id": "tcp-banner",
  "type_version": 1,
  "config": {
    "listeners": [
      {"name": "demo", "port": 2222, "banner": "Welcome to HoneyForge\n", "close_after_banner": false}
    ],
    "logging": {"capture_payload": true, "max_payload_bytes": 4096},
    "management": {"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100}
  }
}
```

2. `POST /api/traps` — создать регистрацию, используя `id` полученного профиля:

```json
{"request_id":"<UUID>","name":"Demo trap","description":"","profile_id":"<PROFILE_ID>"}
```

3. `POST /api/traps/<TRAP_ID>/agent-credentials` с
   `{"expected_generation":0}` — получить `token`, `trap_id`, `agent_ws_url`.
   Передать их агенту через окружение, как показано ниже.
4. `POST /api/traps/<TRAP_ID>/commands` — применить точную ревизию профиля:

```json
{"request_id":"<UUID>","action":"apply_config","params":{"profile_revision":1}}
```

5. Дождаться `status=succeeded` через `GET /api/traps/<TRAP_ID>/commands/<COMMAND_ID>`.
   Затем вызвать тот же POST для создания процесса и открытия портов:

```json
{"request_id":"<UUID>","action":"start","params":{}}
```

После `succeeded` можно подключиться к TCP-порту `2222` на машине агента.
Ловушка отправляет баннер без добавления символов, фиксирует `tcp.connection_opened`,
`tcp.payload_received`, `tcp.connection_closed`. При `capture_payload=false`
метаданные соединений сохраняются. Данные читаются через существующие
`GET /api/events?trap_id=<TRAP_ID>` и `GET /api/events/<EVENT_ID>`.

`stop` с `params:{}` останавливает процесс и закрывает сессии. `apply_config`
при работающей ловушке перезапускает её с новым snapshot и меняет интервал
отправки телеметрии без перезапуска агента. Правка профиля сама
по себе не изменяет процесс. Создание регистрации также не запускает listener.

## Запуск агента

Из `src/backend`:

```powershell
go build -trimpath -buildvcs=false -o edge-worker.exe ./cmd/agent
$env:AGENT_WS_URL = '<agent_ws_url из ответа реквизитов>'
$env:AGENT_TRAP_ID = '<trap_id из ответа реквизитов>'
$env:AGENT_TOKEN = '<token из ответа реквизитов>'
$env:AGENT_REDIS_URL = 'redis://127.0.0.1:6379/0'
# Redis запускается на машине агента:
# docker compose up -d redis
# Для локального backend с собственным CA:
$env:AGENT_CA_FILE = 'C:\certs\localhost-ca.crt'
.\edge-worker.exe
```

На Linux соберите `go build -trimpath -buildvcs=false -o edge-worker ./cmd/agent` и задайте те же
переменные. TLS проверяется; отключения проверки сертификата нет. Требуются
свободные TCP-порты из конфигурации и право bind на них. Один процесс агента
обслуживает одну ловушку; для второй нужны отдельные реквизиты и журнал.

Medium-ловушка Redis использует те же четыре `AGENT_*` переменные и те же
команды `apply_config`, `start`, `stop`. Меняются только `type_id` и `config`
профиля; сам эмулятор собирается как отдельное приложение из `src/redis-trap`.
Пример взаимодействия и событий — в [README эмулятора](../../../redis-trap/README.md).

## Доставка и восстановление

Агент фиксирует событие в локальном Redis до ack ловушке. Обязательны
`appendonly yes`, `appendfsync always` и `maxmemory-policy noeviction`: агент
проверяет их перед запуском. Compose содержит сервис Redis с этими настройками
и постоянным томом. Redis нужен на каждой машине агента, отдельно от приманки
redis-emulator. `AGENT_REDIS_URL` может содержать реквизиты доступа; дочерний
процесс их не получает. Для удалённого Redis используется `rediss://`.

События отправляются с постоянными `batch_id`/`event_id` и удаляются из очереди
только после коррелированного `telemetry.ack` после PostgreSQL commit. При
разрыве WSS они повторяются после reconnect с задержкой 1–30 секунд и jitter.
Код 4401/401 прекращает попытки до замены реквизитов; 4409 завершает вытесненный
агент. Замена токена не меняет ключ журнала, который связан с `AGENT_TRAP_ID`.

Redis сохраняет snapshot, running/stopped, намерения, результаты команд и
очередь событий. Перед hello восстанавливаются конфигурация и listeners для
сохранённого running; незавершённый stop не открывает порты. Старый snapshot
из welcome не заменяет установленную конфигурацию. Результат повторяется до
command.ack, при новой lease повторный dispatch использует журнал без второго
выполнения завершённой команды. Оборванные сессии получают финальное событие
service_stopped с уже зафиксированными байтами.

Очередь ограничена 64 MiB, с резервом для завершения активных сессий. Переполнение
сообщается как buffer_state=full, отказ Redis — unavailable; listeners
останавливаются, ID сохраняются. После восстановления Redis очередь досылается,
а для возобновления остановленного из-за ошибки сбора требуется start.
Атомарная компактизация сохраняет все неподтверждённые события, активные сессии,
snapshot и результаты. Lease запрещает двум процессам владеть одним журналом.
Файловый адаптер остаётся для изолированных тестов; CLI агента требует Redis.

## Проверки

```powershell
go test ./internal/agent ./internal/tcptrap
go test -tags=integration ./internal/agent -run TestDemoTrapToBackend -count=1
go test -tags=integration ./internal/app -run '^TestDemoRegistrationToTCPStop' -count=1 -v
go fmt ./...
go vet ./...
go test ./...
go build ./...
go test -race ./...
```

Smoke-тест запускает настоящий дочерний процесс, TCP listener, локальный WS,
TLS/WSS до тестового backend peer и проверяет доставку трёх событий. Для него
не нужны PostgreSQL/Kafka. Проверки реального backend/БД остаются в
`internal/app` с тегом `integration` и требуют `TEST_DATABASE_URL`.

`TestDemoRegistrationToTCPStop` требует `TEST_DATABASE_URL`, `TEST_KAFKA_BROKERS`
и использует Redis при заданном `TEST_REDIS_URL`. Для полной приёмки задайте все три.
Он проходит регистрацию и повторный вход, создаёт профиль и ловушку, запускает
настоящего агента, отправляет данные в TCP listener, сверяет три события через
REST после Kafka/PostgreSQL, доставку и replay dashboard, ревизию 2, stop/start,
закрытие порта и удаление с сохранением истории событий и команд.
`TestDemoRegistrationToTCPStopAfterWSSLoss` повторяет этот путь с разрывом WSS
после TCP-атаки: агент переподключается, три события появляются ровно по одному
разу, а завершённые команды не меняют статус и время завершения.

`TestRedisServerCrashRestoresDurableEventsAndResults` запускает отдельный Redis,
аварийно завершает его и проверяет восстановление. Укажите путь к redis-server
в `TEST_REDIS_SERVER_PATH`. Обычные Redis-тесты требуют `TEST_REDIS_URL`.
