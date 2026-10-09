# HoneyForge

Решение от команды Seg_Fault.

## Backend

Go API находится в [src/backend](src/backend/README.md). Модуль размещён в `src/backend`, точка входа — `src/backend/cmd/api/main.go`, auth-модуль — `src/backend/modules/auth`.

Состав Go-модуля:

```text
src/backend/
  cmd/api/main.go
  internal/app/
  internal/platform/httpx/
  modules/auth/
  api/openapi.yaml
  migrations/
```

## PostgreSQL для локальной разработки

Docker Compose в корне проекта запускает PostgreSQL и хранит данные в именованном томе. Пароль задаётся локально: скопируйте `.env.example` в игнорируемый Git файл `.env` и измените пароль.

```powershell
docker compose up -d
docker compose ps -a migrate
```

Compose запускает PostgreSQL, затем одноразовый сервис Goose автоматически применяет все миграции из `src/backend/migrations`. Перед запуском API проверьте у `migrate` статус `Exited (0)`. PostgreSQL публикуется только на loopback `127.0.0.1`. Проверки API и запуск описаны в [README backend-модуля](src/backend/README.md).
