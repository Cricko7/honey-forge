# Операторы, организации и cookie-сессии

Контракт: `docs/api/02-auth-organizations.md` и общие правила `docs/api/01-common.md`.

## Архитектура

`cmd/api/main.go → internal/app → modules/auth/http.go → service.go → repository*.go → PostgreSQL`. Go module root: `src/backend`; import path: `github.com/Cricko7/honey-forge/src/backend`.

Auth владеет пользователями, организациями, join-code и сессиями. Gin обрабатывает HTTP и binding; service проверяет бизнес-правила и роли; repository содержит параметризованный SQL и транзакции. Общие ответы находятся в `internal/platform/httpx`. Исходный учебный `main.go` оставлен отдельно.

## Конфигурация

| Переменная | Назначение |
|---|---|
| DATABASE_URL | Обязательная строка подключения PostgreSQL |
| ALLOWED_ORIGIN | Обязательный точный HTTPS-origin фронтенда, например https://localhost:8443; без пути и завершающего / |
| HTTP_ADDR | Адрес HTTP-сервера за HTTPS-прокси, по умолчанию :8080 |
| LOG_LEVEL | debug, info, warn или error; по умолчанию info |

HTTPS завершается на обратном прокси; frontend и REST доступны на одном origin. Cookie всегда Secure. JWT_SECRET и COOKIE_SECURE больше не используются. Поддельный Host или X-Forwarded-For не заменяет ALLOWED_ORIGIN или IP клиента. Доверенные прокси выключены: при реальном прокси IP и лимитер нужно настроить под доверенные адреса этого развёртывания.

## Миграции

Установить Goose отдельно и применить миграции перед запуском API:

```powershell
go install github.com/pressly/goose/v3/cmd/goose@latest
goose -dir .\migrations postgres "$env:DATABASE_URL" up
```

API не применяет миграции автоматически. Не передавать полный файл миграции в psql: он содержит и Up, и Down.

Миграция `20261009000100_operator_sessions.sql` сохраняет существующих пользователей, организации и bcrypt-хеши; владельцы прежних организаций становятся admin. Каждой организации присваивается новый код. Старые JWT больше не принимаются — нужен login. Таблица refresh_sessions оставлена только для отката и не читается новым auth.

Названия старых организаций длиннее 100 символов необходимо согласованно сократить перед миграцией: миграция остановится и откатится, а данные не будут обрезаны автоматически. Откат к прежней схеме также требует старых ограничений названия (2..120).

## Маршруты

| Метод и путь | Ответ |
|---|---|
| POST /api/registrations | 201 SessionView, Location: /api/session, cookie |
| POST /api/sessions | 201 SessionView, Location: /api/session, новая cookie |
| GET /api/session | 200 SessionView с актуальной ролью, сроком и CSRF |
| DELETE /api/session | 204, отзыв текущей сессии и очистка cookie |
| GET /api/organization | 200 своя организация без кода |
| GET /api/organization/join-code | 200 JoinCode; только admin |
| POST /api/organization/join-code/rotations | 200 новый JoinCode; только admin, CSRF |
| GET /healthz | 200, HTTP-процесс жив; это не проверка базы |

Маршруты прежнего /api/v1/auth удалены. Все query-параметры на новых auth-маршрутах запрещены.

Создание организации:

```json
{"email":"admin@example.com","password":"demo-password-2026","organization":{"mode":"create","name":"Demo SOC"}}
```

Вступление:

```json
{"email":"viewer@example.com","password":"demo-password-2026","organization":{"mode":"join","join_code":"0123456789abcdefghijklmnopqrstuv"}}
```

Создание назначает admin; вступление назначает viewer. role, user_id и organization_id не принимаются. Название не уникально и не используется для вступления. Код многоразовый, действует до ротации. Регистрация не идемпотентна: при потерянном ответе выполнить login.

## Валидация и безопасность

- Email: корректный ASCII email без display name, до 254 символов после trim; lowercase перед сравнением и возвратом; глобально уникален.
- Пароль: 12..128 Unicode-символов, максимум 512 UTF-8 байт. Не нормализуется и не обрезается. Новые пароли хешируются Argon2id с солью, 64 MiB, 3 прохода и 4 потока; старые bcrypt-хеши поддерживаются при login.
- Название: 1..100 символов после trim.
- Код: 32 символа base64url. revision: 1..2147483647.
- JSON: максимум 256 KiB; неизвестные/повторяющиеся поля, неправильные типы, null и несколько документов дают 400 invalid_json. Ограничения значений и отсутствующие обязательные поля — 422 validation_failed. Непустые тела требуют application/json.
- Для всех изменяющих запросов обязателен ровно один Origin, в точности равный ALLOWED_ORIGIN. Отсутствующий/чужой Origin — 403 origin_not_allowed, включая logout без сессии.
- Для записи с активной сессией нужен X-CSRF-Token из SessionView. GET не требует CSRF. Registration/login проверяют Origin и application/json, но не требуют предварительного CSRF.
- Registration/login разделяют лимит 10 попыток за 60 секунд на IP независимо от успеха; 429 содержит Retry-After. Лимитер локален одному процессу.
- __Host-session: Path=/, Secure, HttpOnly, SameSite=Strict, без Domain, Max-Age=604800. Срок 7 суток не продлевается чтением. В PostgreSQL сохраняется SHA-256-хеш cookie и CSRF, значения сессии в JSON отсутствуют. Logout удаляет cookie с Max-Age=0.
- Успешный login атомарно отзывает прежнюю сессию текущего браузера и создаёт новую. Ошибка credentials/базы сохраняет старую сессию. Другие браузеры продолжают работать.
- Authorization: Bearer и старые cookies не аутентифицируют оператора.

## Ошибки

Ответ: `{"error":{"code":"...","message":"...","fields":[{"path":"/email","code":"...","message":"..."}]},"request_id":"UUID"}`. fields необязателен, до 20 элементов. X-Request-ID совпадает с request_id.

| HTTP | code | message |
|---|---|---|
| 400 | invalid_json | Invalid JSON body |
| 400 | invalid_query | Invalid query parameters |
| 401 | unauthenticated | Authentication required |
| 401 | invalid_credentials | Invalid email or password |
| 403 | forbidden | Insufficient permissions |
| 403 | csrf_failed | CSRF validation failed |
| 403 | origin_not_allowed | Origin is not allowed |
| 404 | resource_not_found | Resource not found |
| 405 | method_not_allowed | Method not allowed |
| 409 | email_in_use | Email is already registered |
| 409 | already_authenticated | Already authenticated |
| 409 | join_code_changed | Organization join code has changed |
| 409 | revision_exhausted | Resource revision limit reached |
| 413 | body_too_large | Request body is too large |
| 415 | unsupported_media_type | Expected application/json |
| 422 | validation_failed | Request validation failed |
| 422 | invalid_join_code | Invalid organization join code |
| 429 | rate_limited | Too many requests |
| 500 | internal_error | Internal server error |
| 503 | database_unavailable | Database is temporarily unavailable |

Неверный пароль и неизвестный email дают одинаковые code/message. Неизвестный и заменённый join-code также неразличимы. 405 имеет Allow; 429/503 имеют Retry-After.

## Атомарность и подключение других модулей

Создание/вступление, пользователь, сессия и аудит фиксируются одной транзакцией. Join берёт FOR SHARE на строке организации до commit; ротация берёт FOR UPDATE. Поэтому завершённая ротация запрещает старый код; конкурентная ротация одной revision имеет одного победителя. Переполнение revision даёт 409 revision_exhausted.

В auth_audit сохраняются organization.created, organization.viewer_joined, organization.join_code_rotated, session.created и session.revoked. Actor — снимок email/роли; секреты отсутствуют. auth_changes содержит устойчивые audit.created в той же транзакции. Ошибка аудита/уведомления откатывает действие.

Другие operator-модули подключают `Handler.RequireSession()`, затем получают серверный `AuthContext` через `auth.Context(c)`. Для транспорта доступен `Service.ResolveSession(ctx, cookie)`, возвращающий AuthContext через метод, UUID сессии и expires_at; `auth.CheckCSRF` проверяет CSRF.

Модули WSS и общего stream-журнала здесь отсутствуют. Потребитель WSS должен перепроверять ResolveSession и закрывать отозванную сессию с 4401; session.revoked сохранён в аудите/журнале, но доставка и закрытие сокета требуют подключения модуля 09. auth_changes нужно связать с общим журналом модуля 07 при сборке проекта. Сквозной HoneyForge со всеми модулями этим auth не объявляется готовым.

## Логи

slog JSON: UUID запроса, метод, шаблон маршрута, статус, длительность, размер ответа, IP, код ошибки; auth-события содержат user_id/organization_id/роль, ротация — revision. Пароли, cookies, CSRF, join-code, тела и query не логируются. Ошибки PostgreSQL логируются через SQLSTATE без Detail, который может содержать секрет. Паники: тип и стек без значения паники.

## Запуск локально

1. В корне HoneyForge скопировать `.env.example` в `.env` и заменить локальный пароль PostgreSQL.
2. Из корня проекта выполнить `docker compose up -d postgres`.
3. В PowerShell из `src/backend` задать переменные:

```powershell
$env:DATABASE_URL = "postgres://honey_forge:replace-with-local-password@127.0.0.1:5432/honey_forge?sslmode=disable"
$env:ALLOWED_ORIGIN = "https://localhost:8443"
$env:HTTP_ADDR = ":8080"
```

4. Установить Goose и применить миграции из `src/backend`:

```powershell
go install github.com/pressly/goose/v3/cmd/goose@latest
goose -dir .\\migrations postgres "$env:DATABASE_URL" up
```

5. Запустить API командой `go run .\\cmd\\api`. Он слушает HTTP только внутри сети; HTTPS должен завершаться на прокси того же origin, который указан в `ALLOWED_ORIGIN`.

## Проверки без запуска API

```powershell
go fmt ./...
go vet ./...
go test ./...
go build ./...
$env:TEST_DATABASE_URL = "postgres://postgres@127.0.0.1:5432/auth_test?sslmode=disable"
go test -tags=integration ./...
go test -race -tags=integration ./...
```

Integration создаёт отдельную случайную схему в тестовой БД, применяет миграции и удаляет только свою схему. Нужны права CREATE SCHEMA. На Windows race требует CGO и C-компилятор; можно выполнить проверки в golang Linux-контейнере. HTTP проверяется через httptest без открытия порта сервером.
