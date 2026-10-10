# Демо-связка HoneyForge через Docker + Caddy (стаб-бэкенд)

Быстрый показ без Go/PostgreSQL/Kafka: Caddy за единым origin
`http://localhost:8088` отдаёт SPA и проксирует API/WSS на Node-стаб с
синтетическими данными и генератором атак.

```
браузер → Caddy :8088 ─┬─ /api/*  → стаб-бэкенд (Node) :8080
                       └─ /*       → SPA (frontend/)
```

## Запуск (из корня репозитория)
```powershell
docker compose up --build
```
Открыть **http://localhost:8088**. Остановить — `Ctrl+C`, убрать — `docker compose down`.

Для НАСТОЯЩИХ данных и реального Go-бэкенда см. `REAL-STACK.md`.
