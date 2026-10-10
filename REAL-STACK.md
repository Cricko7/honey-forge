# HoneyForge — реальный стек на настоящих данных (nginx + Docker)

Поднимает НАСТОЯЩИЙ Go-бэкенд с PostgreSQL/Kafka/Redis, реальным входом и
персистентностью. Go на хост ставить не нужно — API собирается в Docker.

```
браузер ─https─ nginx :8443 ─┬─ /api/*, /api/stream → Go API :8443 (https, PostgreSQL/Kafka/Redis)
                             └─ /*                   → SPA (собранный фронт из frontend/)
```

> Статус: подготовлено, на машине автора без Docker не запускалось. Первый
> `up --build` может потребовать мелких правок — допилим по логам.

## 0. Предпосылки
- Установлен **Docker Desktop**.
- Образ `golang:1.26-alpine` существует для версии из `src/backend/go.mod` (1.26.1).
  Если тега нет — поправить на доступный патч в `Dockerfile.api`.

## 1. Секреты и сертификаты (из корня репозитория)
```powershell
Copy-Item .env.real.example .env
# сгенерировать два 32-байтных ключа Base64 и вписать в .env:
[Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))  # POSTGRES_PASSWORD
[Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))  # CURSOR_KEY
.\gen-certs.ps1   # self-signed сертификат в ./certs (openssl из Docker)
```

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
4. **Запустить агент** — runtime ловушки:
   - TCP-баннер: `src/backend/cmd/agent` (см. `src/backend/internal/agent/README.md`).
   - Redis Medium: `src/redis-trap` (см. `src/redis-trap/README.md`).
5. **Сгенерировать трафик** — подключиться к порту ловушки → появятся настоящие события.

## 5. Остановка / сброс
```powershell
docker compose -f docker-compose.real.yml down       # остановить
docker compose -f docker-compose.real.yml down -v     # + удалить данные (чистая БД)
```

## Диагностика
- `docker compose -f docker-compose.real.yml logs api` — старт Go API, миграции.
- `docker compose -f docker-compose.real.yml ps` — статусы/healthcheck.
- 502 от nginx → API ещё не поднялся (ждёт healthy БД) или упал на миграциях.

## Демо без Go/Docker-инфры
Для быстрого показа без реального бэка есть стаб на Node: `docker compose up --build`
(Caddy + Node-стаб, синтетические данные) — см. `DOCKER.md`.
