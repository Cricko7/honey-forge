# HoneyForge — Конструктор цепочек ловушек (Python-модуль)

Автономный модуль визуального конструктора ложной инфраструктуры (attack-path) для
платформы HoneyForge. В стиле конструкторов ботов (n8n / Flowise), но для deception:
**бесконечная доска**, узлы-ловушки, рёбра-переходы, ветвления, симуляция пути
атакующего с маппингом на MITRE ATT&CK.

> Данные пока **синтетические (fake)** — модуль самодостаточен и не требует боевого
> Go-бэкенда. Это реализация Фазы 1 роадмапа (`_hf_clone/ROADMAP.md`).

## Запуск

```powershell
# из корня PythonProject (venv уже содержит fastapi+uvicorn)
powershell -File honeyforge\constructor\run.ps1
# или вручную:
.\.venv\Scripts\python.exe -m uvicorn honeyforge.constructor.backend.app:app --port 8077 --app-dir .
```
Открыть **http://127.0.0.1:8077**

## Возможности

**Доска (frontend, vanilla JS):**
- Бесконечный canvas: панорама (тащить фон), зум к курсору (колесо), вписать, миникарта.
- Палитра узлов по категориям + поиск; drag-n-drop на доску, двойной клик — в центр.
- Узлы с портами (вход/выход, типизированные: flow / creds / token / loot), bezier-рёбра,
  подписи на рёбрах (условия перехода), удаление связи кликом.
- Инспектор справа: динамическая форма конфигурации из схемы типа (text/number/select/
  toggle/tags/secret/textarea), переименование узла, удаление.
- Несколько цепочек, переключение, создание из шаблона, дублирование, удаление.
- Автосохранение (debounce PATCH), светлая/тёмная тема (SPI-дизайн), горячие клавиши.
- **Симуляция атаки**: анимация движения атакующего по узлам/рёбрам + таймлайн kill-chain.
- **Валидация** с подсветкой проблемных узлов.

**Бэкенд (FastAPI, Python):**
- `node_registry.py` — каталог 16 типов узлов (точки входа, SSH/RDP/SMB/HTTP/Redis/SQL,
  endpoint-приманки, honeytoken-ы, логика/ветвления, реакции alert/SIEM) со схемами конфига.
- `models.py` — Pydantic-модели (Node/Edge/Chain/Viewport).
- `store.py` — in-memory + JSON-persistence (`data/chains.json`), сид демо-цепочками, шаблоны.
- `validation.py` — правила: точка входа, совместимость портов, висячие узлы, конфликт
  TCP-портов, наличие реакции.
- `simulator.py` — обход графа от точки входа, перенос украденных honeytoken-кредов по
  цепочке, таймлайн с (условным) маппингом на MITRE ATT&CK.
- `app.py` — REST API + раздача статики.

## API
```
GET    /api/registry                     каталог типов узлов + категории
GET    /api/templates                    встроенные шаблоны сценариев
GET    /api/chains                        список цепочек
POST   /api/chains                        создать (поле template — из шаблона)
GET    /api/chains/{id}                   получить
PATCH  /api/chains/{id}                   обновить (nodes/edges/viewport/name…)
DELETE /api/chains/{id}                    удалить
POST   /api/chains/{id}/duplicate         дублировать
POST   /api/chains/{id}/validate          валидация
POST   /api/chains/{id}/simulate          симуляция пути атакующего
```

## Дальнейшее (не сделано)
- AI-ассистент конструктора (генерация цепочки по описанию сети) — Фаза 3.
- Исполнение цепочки на боевом Go-бэкенде (связанные креды, реальный деплой) — Фаза 1 backend.
- Undo/redo, выделение рамкой нескольких узлов, экспорт схемы в PNG/JSON.
