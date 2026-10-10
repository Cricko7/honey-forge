# HoneyForge Frontend

Фронтенд веб-конструктора ловушек (honeypot) для платформы HoneyForge.
Построен строго под контракт `../файлы/api/openapi.yaml`.

## Стек

- **React 18 + Vite + TypeScript**
- **React Flow** — конструктор сети организации (drag-and-drop узлы-ловушки)
- **TanStack Query** — кэш REST, работа с revision/ETag/пагинацией
- **Tailwind CSS** — dark UI + glassmorphism, фиолетовые акценты
- **Recharts** — графики активности атак и типов событий
- **React Router** — навигация

## Запуск

```powershell
npm install
npm run dev
```

Откроется на http://127.0.0.1:3000 (совпадает с `BROWSER_ORIGINS` backend).

## Режимы данных

- **Demo (по умолчанию)** — `VITE_API_BASE` пустой. Работает in-memory mock-слой
  (`src/api/mock.ts`) с генератором live-атак: дашборд, конструктор и события
  наполняются реальными данными без backend.
- **Реальный backend** — задайте `VITE_API_BASE=https://api.example.org`
  в `.env`. Клиент (`src/api/client.ts`) переключится на fetch + WSS `/api/stream`.

## Структура

```
src/
  api/        клиент, хуки React Query, mock-слой, каталог типов
  components/  Layout, UI-примитивы, Modal
  pages/       Login, Dashboard, NetworkBuilder, Traps, TrapDetail, Events, Profiles, Catalog
  lib/         утилиты форматирования
  types.ts     DTO 1:1 с OpenAPI
```

## Экраны

- **Дашборд** — статистика, активность атак (24ч), типы событий, live-лента, топ источников
- **Конструктор сети** — карта ловушек, узлы подсвечиваются красным при атаке в реальном времени
- **Ловушки** — список + детали (команды start/stop/apply_config, статус агента, события)
- **События** — журнал атак, в деталях — декод payload, перехваченные креды, страна источника
- **Профили / Каталог** — конфиги и immutable-типы (tcp-banner/1, redis-emulator/1)

## Подвязка backend

Контракт реализован в `client.ts`: cookie-сессия (`credentials: include`),
`X-CSRF-Token` + `Origin` на мутациях, `X-Expected-Revision` при DELETE ловушки,
`request_id` для идемпотентных POST, WSS `dashboard-stream.v1` (replay→ready→live).
