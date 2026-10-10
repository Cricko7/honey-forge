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

SSH high-interaction добавлен отдельно: [запуск, запись сессий и ограничения](api/13-high-interaction.md).
Его реальный Docker-сценарий проверяется отдельным acceptance-тестом в CI;
локальная среда разработки без Docker не подтверждает запуск этого сценария.

## 0. Предпосылки
- Установлен **Docker Desktop** (на WSL2-бэкенде нужен установленный дистрибутив и
  `wsl --set-default-version 2`, иначе движок не стартует).
- Образ `golang:1.26-alpine` существует для версии из `src/backend/go.mod` (1.26.1).
  Если тега нет — поправить на доступный патч в `Dockerfile.api`, `deploy/decoys/Dockerfile` и `deploy/decoys/Dockerfile.orchestrator`.

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
3. **Дождаться запуска** — основной Compose сам создаёт контейнер ловушки,
   применяет конфигурацию и запускает listener. Статус виден в карточке ловушки.
4. **Сгенерировать трафик** — подключиться к порту ловушки → появятся настоящие события.

Размещение на одном или нескольких хостах описано в [руководстве оркестратора](deploy/decoys/README.md).

### Добавление ловушки скриптом (панель поднимет её сама)

Вместо кликов в UI ловушку можно завести одной командой — скрипт логинится,
создаёт профиль и ловушку через REST, а оркестратор автоматически разворачивает
контейнер приманки и запускает listener. `--wait` дожидается статуса running.

Windows (PowerShell):
```powershell
# первый запуск на чистой базе — заодно регистрирует организацию:
./scripts/add-trap.ps1 -Register -Org "OOO Perimetr" `
    -Email admin@perimetr.ru -Password demo-password-2026 `
    -Name "SSH-DMZ" -Type tcp-banner -Port 2222 -Wait
# ещё одна ловушка существующим админом (другой порт!):
./scripts/add-trap.ps1 -Email admin@perimetr.ru -Password demo-password-2026 `
    -Name "Redis-prod" -Type redis-emulator -Port 6380 -Wait
```

Linux/macOS (bash):
```bash
./scripts/add-trap.sh --register --org "OOO Perimetr" \
    --email admin@perimetr.ru --password demo-password-2026 \
    --name SSH-DMZ --type tcp-banner --port 2222 --wait
./scripts/add-trap.sh --email admin@perimetr.ru --password demo-password-2026 \
    --name Redis-prod --type redis-emulator --port 6380 --wait
```

Параметры: `-Type tcp-banner|redis-emulator`, `-Port` (у каждой ловушки свой,
публикуется на хост), `-Banner` (для tcp-banner), `-RedisPassword`, `-ConfigFile`
(свой JSON config под схему типа). Трафик на опубликованный порт → события в дашборде.

## 5. Остановка / сброс
```powershell
docker compose -f docker-compose.real.yml down       # остановить
docker compose -f docker-compose.real.yml down -v     # + удалить данные (чистая БД)
```

## Диагностика
- `docker compose -f docker-compose.real.yml logs api` — старт Go API, миграции.
- `docker compose -f docker-compose.real.yml ps` — статусы/healthcheck.
- 502 от nginx → API ещё не поднялся (ждёт healthy БД) или упал на миграциях.
