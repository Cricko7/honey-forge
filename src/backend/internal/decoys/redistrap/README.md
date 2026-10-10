# Medium-ловушка Redis

`redis-emulator/1` эмулирует RESP2 и inline-команды Redis без настоящего Redis,
доступа к файловой системе или исходящих подключений. Данные фиктивны и живут
только в памяти одной сессии. Доступны `PING`, `ECHO`, `AUTH`, `INFO`, `DBSIZE`,
`KEYS`, `GET`, `SET`, `DEL`, `EXISTS`, `SELECT 0`, `QUIT`. До успешного `AUTH`
команды получают `NOAUTH`. Для `AUTH` допускаются формы `AUTH password` и
`AUTH default password`. Неизвестные и неверно сформированные команды дают
ошибку, не выполняются на хосте агента.

## Профиль и запуск

Создайте профиль через тот же `POST /api/profiles`, что и для `tcp-banner`:

```json
{
  "request_id": "<UUID>",
  "name": "Redis medium",
  "description": "Интерактивная Redis-приманка",
  "type_id": "redis-emulator",
  "type_version": 1,
  "config": {
    "services": [{"name": "redis", "port": 6380, "password": "bait-pass"}],
    "management": {"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100}
  }
}
```

`password` — учётные данные приманки. Поле `writeOnly`: в ответах профиля
значение скрыто, а агент получает полный snapshot. Затем создайте Trap, выдайте
реквизиты и отправьте команды
`apply_config` с `{"profile_revision":1}` и `start` с `{}` по шагам в
[руководстве агента](../../agent/README.md). `stop` тоже принимает `{}`.

Соберите единый агент из `src/backend` командой
`go build -o edge-worker.exe ./cmd/agent` (Linux: `go build -o edge-worker ./cmd/agent`). Запускайте
его с теми же `AGENT_WS_URL`, `AGENT_TRAP_ID`, `AGENT_TOKEN`,
`AGENT_REDIS_URL` и, при собственном CA, `AGENT_CA_FILE`, что и TCP-агент.
Для этой ловушки нужен собственный Trap ID и токен; Redis хранит её журнал
в отдельном ключе. Приложение
само запускает дочерний процесс Redis после команды `start`.

После `start` подключитесь к порту 6380 на хосте агента. Например, через
`redis-cli -h <хост> -p 6380` последовательно введите `AUTH bait-pass`,
`KEYS *`, `GET app:mode`, `SET demo value`, `GET demo`, `DEL demo`, `QUIT`.
Ответы отражают изменения только в текущей сессии.

Ловушка фиксирует `service.connection_opened`, каждый `service.auth_attempt`,
каждую `service.action` и `service.connection_closed`. События содержат
source IP/port, destination port, время, session_id/sequence, введённые
логин/пароль, текст команды, результат, объём принятого запроса, длительность
и причину завершения. События идут через тот же локальный журнал и WSS-канал
агента, что и TCP-баннер; читать их можно через `GET /api/events?trap_id=...`.
Журнал содержит введённые атакующим пароли и команды, поэтому храните его в
закрытом каталоге вне репозитория.

Лимит — 64 одновременные сессии, 30 секунд простоя и 8192 байт на запрос.
RESP3 и реальные команды Redis, файловая система и сеть за пределами
слушающего порта не эмулируются.

Локальные тесты без внешнего Redis: из `src/backend` выполните
`go test ./internal/decoys/redistrap`. Они проверяют последовательность команд, ответы и созданные
события через локальное подключение к эмулятору.
