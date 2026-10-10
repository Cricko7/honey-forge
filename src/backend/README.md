# Операторы, профили и жизненный цикл ловушек

Auth/organizations и profiles подключены к общей сборке common/catalog.
Ловушки (05), реквизиты агента, команды (06), PostgreSQL-приём и история событий
подключены к общей сборке. `/assets/stream` использует настоящие token/generation,
состояние ловушки и транзакционное хранилище событий. Без реквизитов handshake
возвращает 401. Контракт и проверки: [modules/traps/README.md](modules/traps/README.md),
[modules/events/README.md](modules/events/README.md), [modules/agentws/README.md](modules/agentws/README.md).
Единственный Go-модуль и единственная сборка API находятся в этой директории.
Миграции: `../../migrations/`; полный OpenAPI: [../../api/openapi.yaml](../../api/openapi.yaml).

Основной запуск с TLS описан в [корневом README](../../README.md):
`go run ./cmd/api` из `src/backend`. Docker Compose поднимает PostgreSQL и Kafka;
Goose-миграции применяются при запуске API.
Kafka требует `KAFKA_BROKERS`; подтверждение телеметрии выдаётся после commit
PostgreSQL, а не после Kafka ACK. Подробности: [events](modules/events/README.md).

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

### Назначение модулей

**Auth** отвечает на вопрос «кто делает запрос и к какой организации он
относится». Он регистрирует и аутентифицирует пользователей, создаёт сессии,
проверяет cookie и CSRF, назначает роли admin/viewer и управляет кодом
приглашения в организацию.

**Catalog** отвечает на вопрос «какие типы ловушек поддерживает система и как
настроить каждый из них». Он публикует неизменяемые версии описаний типов и их
JSON Schema, а также проверяет конфигурации, события и команды по выбранной
версии.

**Profiles** хранит пользовательские настройки ловушки на основе версии из
catalog. Он проверяет права через auth, проверяет конфигурацию через catalog,
сохраняет изменения и их ревизии, скрывает секреты в ответах и поддерживает
безопасное параллельное редактирование через `If-Match`.

Вместе модули проводят запрос от идентификации оператора и проверки его роли
через выбор поддерживаемого типа и проверку его схемы к сохранению профиля
в организации.

Проверки выполняются из `src/backend`: `go fmt ./...`, `go vet ./...`, `go test ./...`,
`go build ./...`. С отдельной TEST_DATABASE_URL: `go test -tags=integration ./...`
и `go test -race -tags=integration ./...` (для Windows необходим C compiler).
Тесты создают изолированные схемы.

### Проверки по модулям

#### Auth — пользователи, организации и сессии

Обычные тесты проверяют регистрацию, создание организации и вступление по
join-code, роли admin/viewer, login/logout, CSRF, срок действия сессии, ротацию
кода, проверку пароля, ограничения HTTP-запросов и безопасную обработку ошибок.
PostgreSQL-тесты дополнительно проверяют конкурирующие регистрации и ротации,
ожидание блокировки при вступлении, полный цикл миграций и сохранение учётных
записей при переходе со старой схемы. Сценарии с ошибкой записи аудита проверяют,
что обновление сессии откатывается целиком.

```powershell
go test ./modules/auth/...
go test -tags=integration ./modules/auth/repository -run '^TestPostgres' -count=1
```

#### Catalog — версии типов и JSON Schema

Тесты проверяют поиск точной версии, неизменность снимков и ETag, пагинацию,
условные HTTP-ответы и соответствие встроенных схем API-контракту. Отдельные
проверки валидируют config, telemetry, действия и границы захвата TCP payload.
Интеграционный тест с PostgreSQL проверяет повторную и конкурентную установку
версий, запрет изменения опубликованной версии и откат неполной установки.

```powershell
go test ./modules/catalog
go test -tags=integration ./modules/catalog -run '^TestRepositoryInstall$' -count=1
```

#### Profiles — настройки ловушек и ревизии

Тесты сервиса и HTTP проверяют создание, чтение, изменение и удаление профилей,
проверку конфигурации через каталог, сохранение и удаление `writeOnly`-секретов,
повторный `request_id`, права ролей, точный `If-Match`, ошибки и пагинацию.
PostgreSQL-тесты проверяют историю ревизий, конкурентный `request_id` и PATCH,
атомарный откат при ошибках аудита или уведомлений, границы курсора и гонку между
назначением профиля ловушке и удалением профиля.

```powershell
go test ./modules/profiles/...
go test -tags=integration ./modules/profiles/repository -run '^TestPostgres' -count=1
```

Интеграционные команды обращаются к PostgreSQL через `TEST_DATABASE_URL`,
описанный ниже. Отдельные команды запускают тесты только выбранного модуля;
общий поток между модулями проверяется в разделе сквозных тестов.

### Сквозные тесты модулей

Сценарии в `internal/app/*integration_test.go` запускают собранный `app.Open`
с настоящими auth и catalog сервисами, профилями и PostgreSQL. Они проверяют
регистрацию admin и viewer, получение версии типа из каталога, полный цикл
профиля и отказ на неверной конфигурации, повторный `request_id`, ревизии и
`If-Match`, права и изоляцию организаций, `Origin`/CSRF и отзыв сессии.
Отдельные сценарии проверяют курсор между организациями и границей новых
записей, а также конкурентное создание и изменение профиля: один запрос
побеждает, остальные получают replay или конфликт; аудит, история и события
записываются атомарно.

Тесты проходят через `httptest`, без запуска API-процесса. Для каждого сценария
создаётся отдельная PostgreSQL-схема, в неё применяются Goose-миграции, а после
теста схема удаляется. Укажите отдельную тестовую базу и запускайте из backend:

```powershell
$env:TEST_DATABASE_URL = 'postgres://user:password@localhost:5432/test_db?sslmode=disable'
go test -tags=integration ./internal/app -run '^TestReal' -count=1
```

`go test -tags=integration ./... -count=1` запускает все интеграционные проверки
репозитория, а `go test -race -tags=integration ./... -count=1` дополнительно
проверяет конкурентный код детектором гонок.

Frontend replay/live подключён к транзакционному журналу: [модуль 09](modules/frontendws/README.md).
Оба runtime находятся в `internal/decoys`; `cmd/agent` запускает нужный тип по
snapshot, а `cmd/orchestrator` разворачивает отдельные Swarm-контейнеры.
