# HoneyForge

[Порядок модулей 01–11 и состояние реализации](api/README.md) ·
[Стиль кода и проверки](CONTRIBUTING.md)

Решение команды Seg_Fault. Общий модуль 01 реализует транспортные правила,
валидацию, доступ, WSS Envelope, идемпотентность и согласованную фиксацию
изменений. Проверяемое соответствие требованиям: [api/01-acceptance.md](api/01-acceptance.md).
Контракт: [api/01-common.md](api/01-common.md), [OpenAPI](api/openapi.yaml).

Модуль 03 добавляет immutable-каталог типов, GET `/api/trap-types` и точной
версии, snapshot pagination/ETag и валидацию config/event/action для tcp-banner/1.
Требования и границы подключения: [api/03-catalog.md](api/03-catalog.md),
[api/03-acceptance.md](api/03-acceptance.md). Auth/organizations и profiles из
`src/backend/modules` подключены к тому же серверу. Каталог использует настоящую
cookie-сессию; профили проверяются его точной версией схемы.

## Запуск

Go 1.26+, PostgreSQL 17+, TLS-сертификат и ключ. Docker Compose запускает
только PostgreSQL; процесс Go запускается локально.

```powershell
# Первичная локальная настройка. Сохраните секреты вне Git и повторно используйте
# те же значения при перезапуске; особенно CURSOR_KEY и POSTGRES_PASSWORD.
$env:POSTGRES_PASSWORD = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
$env:CURSOR_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
$env:DATABASE_URL = "postgres://honey_forge:$([Uri]::EscapeDataString($env:POSTGRES_PASSWORD))@127.0.0.1:5432/honey_forge?sslmode=disable"
$env:BROWSER_ORIGINS = 'https://localhost:3000'
$env:TLS_CERT_FILE = 'C:\certs\localhost.crt'
$env:TLS_KEY_FILE = 'C:\certs\localhost.key'
$env:API_ADDR = ':8443'
$env:GIN_MODE = 'release'
docker compose up -d postgres
Set-Location src/backend
go run ./cmd/api
```

`sslmode=disable` в примере относится только к локальному соединению PostgreSQL.
HTTP-сервер работает исключительно по HTTPS. При запуске создаются pgx pool,
Goose migrations, браузерная политика, cursor codec, журнал изменений и
координатор записей. Отсутствующие настройки останавливают запуск.

Go-модуль и единственный API entrypoint находятся в `src/backend/`.
Миграции находятся в корневом `migrations/`, полный контракт — в
`api/openapi.yaml`. API запускает auth/organizations, profiles и catalog. Регистрация создаёт
реальную сессию; viewer читает каталог и профили, admin также изменяет профили.
Ловушки, реквизиты агента, команды и durable-приём событий подключены к API;
состояние и проверки описаны в [модуле traps](src/backend/modules/traps/README.md).
Отдельный TCP runtime агента и frontend WSS остаются отдельными компонентами.

## Подключение feature

- `app.Open` возвращает `Runtime`: Router, Mutations, Schemas, Cursors, Browser, Catalog.
  Каталог устанавливается после миграций. Опубликованные версии нельзя изменить
  или исключить при перезапуске; новую версию добавляют отдельным Definition.
  Реальный auth repository/service и адаптер `contract.Principal` подключаются
  при запуске; `ProfileTypeLookup` использует тот же Runtime.Catalog.
- `app.RegisterOperator` получает реальный `Authenticate` модуля 02 и выполняет
  authentication → Origin/CSRF → role → ownership hooks → RESTBody → handler.
  Ownership hook вызывает проверку сервиса до binding; условия повторно
  проверяются внутри общей транзакции перед побочным эффектом.
- `contract.BindJSON` использует Gin `ShouldBindJSON`. Он ограничивает тело
  256 KiB, проверяет media type, UTF-8, дубли ключей на любой глубине, единственный
  JSON-документ, точные ключи/типы и затем declarative `validate` tags.
- DTO используют общие `ID`, `Name`, `Description`, `TypeID`, `Action`,
  `EventType`, `Revision`, `TypeVersion`, `Timestamp`, `Bytes`.
  `Name` нормализует внешние пробелы. Обязательные непустые значения имеют
  `validate:"required"`; обязательное значение с допустимым zero value,
  например Description, — `validate:"present"`. Обязательное `T|null` задаётся
  `Nullable[T]` с `validate:"present"`. Это различает null и отсутствие поля.
- Общие ответы: `CreatedOrReplayed` (201 + Location / 200), `NoContent`,
  `ProfileResponse` (ETag), `CatalogResponse` (private cache + ETag + 304), `Fail`.
  Location указывает на реально созданный GET-ресурс feature.
- `RequestQuery` отвергает malformed encoding, неизвестные/повторные ключи.
  `ParseListQuery` задаёт limit 1..100/default 50; временные фильтры принимаются
  только явно. `ParseTimeRange` использует включённый from/исключённый to.
- CursorCodec использует постоянный 32-байтовый ключ и связывает позицию с
  организацией, коллекцией, каноническими фильтрами и границей первой страницы.
  Feature применяет эту границу к своему SQL и задаёт фиксированную сортировку.
- `SchemaStore` хранит immutable Draft 2020-12 версии в PostgreSQL.
  `configschema.Compile` разрешает только локальные `$ref`/`$defs` и запрещает
  загрузку внешних ресурсов. `Schema.Validate` возвращает безопасные поля ошибок.
  Catalogue DTO и конкретные config/data/params schemas принадлежат 03.
- `Schema.MergeConfig` сохраняет пропущенные writeOnly-поля, проверяет
  `clear_secret_fields`, размер полного эффективного config (128 KiB) и схему.
  Одновременное назначение и очистка одного секрета даёт **422 validation_failed**.
  `PublicConfig` вырезает только writeOnly-поля и возвращает `secret_fields_set`;
  захваченные атакующие пароли не маскируются без writeOnly в их схеме.
- `Mutations.Create` принимает серверную организацию/маршрут, request_id и
  нормализованный JSON. Ключи и fingerprint сохраняются в PostgreSQL. Для Command
  передаётся TrapID как область ключа. При replay feature читает текущий ресурс
  по Result.ResourceID и возвращает 200, без повторного выполнения изменения.
- `Mutations.Write/Create/Delete` выполняют callback feature через один pgx.Tx
  вместе с обязательными audit и change records. Metadata — ограниченная
  типизированная структура без свободного payload, config, cookies и tokens.
  Ошибка любой записи откатывает всё; Delete сохраняет tombstone ключа.
- `ExpectedProfileRevision` / `ExpectedTrapRevision` разбирают заголовки.
  `Mutations.UpdateRevision` атомарно читает, сравнивает и увеличивает revision,
  затем выполняет изменение и фиксацию. Lifecycle/ownership SQL feature выполняется
  в том же callback/Tx. Delete проверяет expected revision/lifecycle там же.
- `Mutations.AgentWrite` принимает только закреплённую agent identity своей
  ловушки и атомарно фиксирует наблюдения/события и notifications. Heartbeat
  и команды используют state_version feature и не вызывают увеличение revision.
- `Mutations.Snapshot` читает REST snapshot и границу журнала в одной
  repeatable-read транзакции. `Changes` читает replay строго внутри организации
  и заданной границы. Порядок sequence соответствует порядку commit.
- `stream.Upgrade` после авторизации создаёт WSS с общими HTTP-ошибками и
  X-Request-ID. Для frontend передаётся Runtime.Browser как Origin policy.
  Compression выключена; frame и полное сообщение ограничены 256 KiB;
  binary закрывается с 1003, превышение — с 1009.
  Socket.Read принимает только клиентские Envelope с reply_to absent/null;
  Reply коррелирует запрос, Notify выдаёт явный reply_to=null.
  Контекст отменяет чтение/запись. Конкретные payload/доставка принадлежат 08/09.
- `stream.NewAgentEndpoint` задаёт неизменяемые HTTPS-origin/path установки
  агента. Profile config не используется для формирования этого endpoint;
  управляющий origin должен быть отдельным от операторского.

## Проверки

```powershell
Set-Location src/backend
go fmt ./...
go vet ./...
go test ./...
go build ./...
# Для SQL-проверок укажите отдельную тестовую PostgreSQL БД:
$env:TEST_DATABASE_URL = '<test PostgreSQL connection string>'
go test ./... -count=1
# На Windows требуется C-компилятор в PATH:
$env:CGO_ENABLED = '1'
go test -race ./... -count=1
```

Без TEST_DATABASE_URL SQL-тесты явно пропускаются. При разработке выполнены
полный прогон с настоящим PostgreSQL, миграции на свежей схеме и race-проверка.
Готовность общих компонентов не объявляет готовой сборку модулей 02–11.
