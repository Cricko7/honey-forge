# HoneyForge — реальный стек на настоящих данных (nginx + Docker)

Поднимает НАСТОЯЩИЙ Go-бэкенд с PostgreSQL/Kafka/Redis, реальным входом и
персистентностью. Go на хост ставить не нужно — API собирается в Docker.

```
браузер ─https─ nginx :8443 ─┬─ /api/*, /api/stream → Go API :8443 (https, PostgreSQL/Kafka/Redis)
                             └─ /*                   → SPA (собранный фронт из frontend/)
```

> Статус: **проверено end-to-end** (Docker 29.x, образ `golang:1.26-alpine` собрался).
> Поднимается `up --build`; регистрация → профиль → ловушка → агент → атака → события
> прошли на реальном бэке.

## 0. Предпосылки
- Установлен **Docker Desktop** (на WSL2-бэкенде нужен установленный дистрибутив и
  `wsl --set-default-version 2`, иначе движок не стартует).
- Образ `golang:1.26-alpine` существует для версии из `src/backend/go.mod` (1.26.1).
  Если тега нет — поправить на доступный патч в `Dockerfile.api`/`Dockerfile.agent`.

## 1. Секреты и сертификаты (из корня репозитория)
```powershell
Copy-Item .env.real.example .env
```
Вписать в `.env` два ключа. ВАЖНО: `POSTGRES_PASSWORD` должен быть URL-безопасным
(символы `+ / =` из Base64 ломают разбор `DATABASE_URL` → `invalid database configuration`),
поэтому для пароля берём HEX, а для `CURSOR_KEY` — ровно 32 байта в Base64:
```powershell
# PowerShell 5.1: у RandomNumberGenerator нет статического GetBytes — через RNGCryptoServiceProvider
$rng = New-Object System.Security.Cryptography.RNGCryptoServiceProvider
$b = New-Object byte[] 24; $rng.GetBytes($b)
($b | ForEach-Object { $_.ToString('x2') }) -join ''                 # POSTGRES_PASSWORD (hex)
$k = New-Object byte[] 32; $rng.GetBytes($k); [Convert]::ToBase64String($k)   # CURSOR_KEY (base64)
.\gen-certs.ps1   # self-signed сертификат в ./certs (openssl из Docker)
```
> Сменили пароль после первого запуска? Том Postgres уже инициализирован старым —
> пересоздать: `docker compose -f docker-compose.real.yml down -v`.

## 2. Запуск
```powershell
docker compose -f docker-compose.real.yml up --build
```
Открыть **https://localhost:8443** (браузер предупредит про self-signed — принять исключение).

## 3. Настоящий вход
База пустая. На экране входа → «Создать» → регистрация администратора
(название организации + email + пароль 12–128 символов). Это `POST /api/registrations`
(mode:create): атомарно создаёт организацию + admin + сессию.

## 4. Данные (почему дашборд сначала пустой)
Настоящих событий атак нет, пока нет запущенной ловушки, которую кто-то «атакует»:
1. **Профиль** — «Профили» → «Новый профиль» (тип + config по схеме).
2. **Ловушка** — «Конструктор сети» → «Добавить ловушку».
3. **Реквизиты агента** — на странице ловушки выдать credentials (token + agent_ws_url).
4. **Запустить агент** — runtime ловушки (без него `start` даёт 409
   `configuration_not_applied`: порт приманки открывает агент, не бэкенд).
   Готовый docker-сервис `agent` (profile `agent`, собирается из `Dockerfile.agent`):
   ```powershell
   # token/trap_id из ответа POST /api/traps/{id}/agent-credentials {"expected_generation":0}
   $env:AGENT_TRAP_ID = "<trap_id>"; $env:AGENT_TOKEN = "<token>"
   docker compose -f docker-compose.real.yml --profile agent up --build -d agent
   ```
   Агент коннектится к `wss://api:8443/assets/stream` (api в SAN серта), журнал в redis БД 1,
   порт приманки 2222 проброшен на хост. Альтернатива — нативно: `src/backend/cmd/agent`
   (см. `src/backend/internal/agent/README.md`); Redis Medium — `src/redis-trap`.
5. **Применить и запустить** — после online-агента:
   `POST /api/traps/{id}/commands` `apply_config {profile_revision:1}` → дождаться
   `succeeded` (`GET .../commands/{cid}`) → `start {}` → `runtime_state: running`.
6. **Сгенерировать трафик** — подключиться к порту ловушки (напр. `2222`) →
   появятся настоящие события `tcp.connection_opened/payload_received/connection_closed`
   в дашборде и `GET /api/events?trap_id={id}`.

> Один агент обслуживает одну ловушку (отдельные реквизиты + журнал). Авто-провижининг
> N приманок на разных портах с проверкой и сбором статистики в дашборд — отдельная задача.

## 5. Остановка / сброс
```powershell
docker compose -f docker-compose.real.yml down       # остановить
docker compose -f docker-compose.real.yml down -v     # + удалить данные (чистая БД)
```

## Диагностика
- `docker compose -f docker-compose.real.yml logs api` — старт Go API, миграции.
- `docker compose -f docker-compose.real.yml ps` — статусы/healthcheck.
- 502 от nginx → API ещё не поднялся (ждёт healthy БД) или упал на миграциях.
