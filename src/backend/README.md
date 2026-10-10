# Операторы, организации и профили

Auth/organizations и profiles подключены к общей сборке common/catalog.
Единственный Go-модуль и единственная сборка API находятся в этой директории.
Миграции: `../../migrations/`; полный OpenAPI: [../../api/openapi.yaml](../../api/openapi.yaml).

Основной запуск с TLS описан в [корневом README](../../README.md):
`go run ./cmd/api` из `src/backend`. Docker Compose поднимает PostgreSQL;
Goose-миграции применяются при запуске API.

Единственный entrypoint использует `internal/app.Open`, реальные PostgreSQL repositories
и одну точную версию каталога. Cookie имеет Secure, HttpOnly, SameSite=Strict,
Path=/, без Domain; срок сессии — семь суток без продления чтением. Пароли
хешируются, cookie и CSRF сохраняются только как хеши. Ответ содержит CSRF,
но не cookie, пароль или join-code посторонней организации.

| Endpoint | Назначение |
|---|---|
| POST /api/registrations | Создать организацию/admin или вступить viewer; 201 и сессия |
| POST /api/sessions | Login; 201 и новая сессия |
| GET /api/session | Текущая сессия и CSRF |
| DELETE /api/session | Отозвать сессию, 204 |
| GET /api/organization | Текущая организация |
| GET /api/organization/join-code | Код приглашения для admin |
| POST /api/organization/join-code/rotations | Ротация кода для admin |
| GET /api/trap-types | Каталог с ETag и snapshot pagination |
| GET /api/trap-types/{type_id}/versions/{type_version} | Точная immutable-версия |
| GET, POST /api/profiles | Список и создание профиля |
| GET, PATCH, DELETE /api/profiles/{id} | Чтение/изменение/удаление профиля |

Все изменяющие запросы требуют разрешённый Origin. Запросы с активной сессией
также требуют X-CSRF-Token. Viewer читает каталог/профили; изменения доступны
admin. Профили используют общий каталог для проверки config и writeOnly-секретов,
If-Match для изменения и request_id для создания. Подробности:
[modules/profiles/README.md](modules/profiles/README.md).

Проверки выполняются из `src/backend`: `go fmt ./...`, `go vet ./...`, `go test ./...`,
`go build ./...`. С отдельной TEST_DATABASE_URL: `go test -tags=integration ./...`
и `go test -race -tags=integration ./...` (для Windows необходим C compiler).
Тесты создают изолированные схемы.

Журналы auth_changes/profile_changes транзакционны; их доставка по WSS,
агентский TCP runtime, команды и catalog.changed пока требуют следующих модулей.
