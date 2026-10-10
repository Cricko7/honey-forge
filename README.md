# HoneyForge

[Порядок модулей 01–11 и состояние реализации](api/README.md) ·
[Стиль кода и проверки](CONTRIBUTING.md)

Решение команды Seg_Fault. Общий модуль 01 реализует транспортные правила,
валидацию, доступ, WSS Envelope, идемпотентность и согласованную фиксацию
изменений. Проверяемое соответствие требованиям: [api/01-acceptance.md](api/01-acceptance.md).
Контракт: [api/01-common.md](api/01-common.md), [OpenAPI](api/openapi.yaml).

Модуль 03 добавляет immutable-каталог типов, GET `/api/trap-types` и точной
версии, snapshot pagination/ETag и валидацию config/event/action для tcp-banner/1
и redis-emulator/1.
Требования и границы подключения: [api/03-catalog.md](api/03-catalog.md),
[api/03-acceptance.md](api/03-acceptance.md). Auth/organizations и profiles из
`src/backend/modules` подключены к тому же серверу. Каталог использует настоящую
cookie-сессию; профили проверяются его точной версией схемы.

## 1. Стек и зависимости

- Go 1.26.1; модули API и отдельной Medium-ловушки находятся в `src/backend`
  и `src/redis-trap`. Версии библиотек закреплены в их `go.mod` и `go.sum`.
- Gin — HTTP API, `pgx/v5` — доступ к PostgreSQL 17, Goose — SQL-миграции.
- `validator/v10` и JSON Schema Draft 2020-12 — проверка входных данных и
  конфигураций; `gorilla/websocket` — WSS, `franz-go` — Kafka.
- Для запуска нужны PostgreSQL, Kafka и TLS-сертификат с закрытым ключом.
  Локальные версии инфраструктуры указаны в `compose.yaml`.

## 2. Как проект собирается

Из корня репозитория перейдите в каталог Go-модуля:

```powershell
Set-Location src/backend
go mod download
go build ./...
go build -o honey-forge-api.exe ./cmd/api
```

На Linux последняя команда может создавать файл без расширения:
`go build -o honey-forge-api ./cmd/api`. Запускайте бинарный файл с рабочим
каталогом `src/backend`: приложение ищет миграции в `../../migrations`.

## 3. Тесты

Из `src/backend` доступны обычные проверки:

```powershell
go fmt ./...
go vet ./...
go test ./...
go build ./...
```

Отдельное приложение Medium собирается и проверяется из `src/redis-trap`:

```powershell
Set-Location ../redis-trap
go test ./...
go build ./...
```

Интеграционные тесты с тегом `integration` используют отдельную PostgreSQL
базу, в которой разрешено создавать схемы. Тест Kafka запускается при наличии
`TEST_KAFKA_BROKERS`; без соответствующих переменных внешние проверки
пропускаются.

```powershell
$env:TEST_DATABASE_URL = 'postgres://user:password@127.0.0.1:5432/honey_forge_test?sslmode=disable'
$env:TEST_KAFKA_BROKERS = '127.0.0.1:29092'
go test -tags=integration ./... -count=1
go test -race -tags=integration ./... -count=1
```

Для `-race` на Windows нужен C-компилятор и `CGO_ENABLED=1`. Подробные проверки
по модулям описаны в [backend README](src/backend/README.md).

## 4. Конфигурация и переменные окружения

| Переменная | Назначение |
|---|---|
| `POSTGRES_PASSWORD` | Обязательный пароль PostgreSQL для Docker Compose. |
| `DATABASE_URL` | Обязательная строка подключения API к PostgreSQL. |
| `CURSOR_KEY` | Обязательный постоянный ключ: 32 байта в Base64; сохраните его между перезапусками. |
| `BROWSER_ORIGINS` | Обязательные разрешённые HTTPS-origin браузера, через запятую. |
| `TLS_CERT_FILE`, `TLS_KEY_FILE` | Обязательные пути к сертификату и закрытому ключу HTTPS. |
| `KAFKA_BROKERS` | Обязательные адреса брокеров Kafka, через запятую. |
| `KAFKA_TELEMETRY_TOPIC` | Тема телеметрии; по умолчанию `honey-forge.telemetry`. |
| `AGENT_WS_URL` | Необязательный внешний WSS URL вида `wss://host/assets/stream`; по умолчанию строится из первого `BROWSER_ORIGINS`. |
| `API_ADDR` | Адрес API; по умолчанию `:8443`. |
| `GIN_MODE` | Для развёрнутого сервера задайте `release`. |
| `TEST_DATABASE_URL`, `TEST_KAFKA_BROKERS` | Только для интеграционных тестов. |

Секреты передаются через окружение и не сохраняются в Git. Для удалённой
PostgreSQL используйте TLS вместо локального `sslmode=disable` из примера ниже.

## 5. Базы данных

PostgreSQL хранит пользователей, сессии, каталог, профили, ловушки, команды,
события и журнал изменений. Goose применяет SQL-файлы из корневого
`migrations/` при запуске API. Kafka служит журналом приёма телеметрии;
подтверждение агенту выдаётся после фиксации события в PostgreSQL.
Docker Compose сохраняет данные PostgreSQL и Kafka в именованных томах
`postgres_data` и `kafka_data`.

## 6. Запуск приложения локально или на сервере

Пример для PowerShell из корня репозитория. Подставьте реальные пути к
TLS-файлам. Значения `POSTGRES_PASSWORD` и `CURSOR_KEY` сгенерируйте один раз,
сохраните вне Git и повторно используйте после перезапуска:

```powershell
$env:POSTGRES_PASSWORD = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
$env:CURSOR_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
$env:DATABASE_URL = "postgres://honey_forge:$([Uri]::EscapeDataString($env:POSTGRES_PASSWORD))@127.0.0.1:5432/honey_forge?sslmode=disable"
$env:BROWSER_ORIGINS = 'https://localhost:3000'
$env:KAFKA_BROKERS = '127.0.0.1:29092'
$env:AGENT_WS_URL = 'wss://localhost:8443/assets/stream'
$env:TLS_CERT_FILE = 'C:\certs\localhost.crt'
$env:TLS_KEY_FILE = 'C:\certs\localhost.key'
$env:API_ADDR = ':8443'
$env:GIN_MODE = 'release'
docker compose up -d postgres kafka
Set-Location src/backend
go run ./cmd/api
```

Сервер слушает HTTPS на `API_ADDR`. На сервере задайте те же переменные,
соберите бинарный файл из `src/backend` и запускайте его с этой же рабочей
директорией через менеджер процессов. Нужны доступ к PostgreSQL и Kafka и
действующие TLS-файлы. Отсутствующие обязательные настройки останавливают запуск.

### Nginx перед API

Ниже — развёртывание на **одном Linux-хосте** (команды установки — для
Debian/Ubuntu): nginx принимает публичный HTTPS/WSS на 443, а Go API слушает
только `127.0.0.1:8443`. Пример конфигурации:
[deploy/nginx/honey-forge.conf](deploy/nginx/honey-forge.conf). Агент и его TCP
ловушки могут находиться на других хостах. Если nginx и API размещены на разных
машинах, этот пример с `127.0.0.1` не подходит: настройте отдельный защищённый
upstream и разрешите к нему доступ только с хоста nginx.

1. **Подготовьте DNS и сертификаты.** Направьте A-запись, например
   `api.example.org`, на хост nginx. Понадобятся действующий публичный
   сертификат с цепочкой и ключом для этого имени, а также отдельный
   сертификат Go API с SAN `DNS:localhost`, выданный вашим внутренним CA.
   Разместите сертификат CA в `/etc/nginx/certs/backend-ca.crt`. Закрытые ключи
   храните вне репозитория с доступом только у нужных процессов. В шаблоне
   публичные файлы указаны как `/etc/nginx/certs/public.crt` и `public.key`;
   можно заменить эти пути на файлы вашего менеджера сертификатов, чтобы
   продление не требовало ручного копирования. Если у вас есть AAAA-запись,
   добавьте в оба `server` блока nginx прослушивание IPv6 либо уберите эту
   запись: шаблон слушает только IPv4.

2. **Настройте и запустите Go API** по разделу выше. В окружении его сервиса
   задайте, помимо `DATABASE_URL`, `CURSOR_KEY` и `KAFKA_BROKERS`:

   ```text
   API_ADDR=127.0.0.1:8443
   TLS_CERT_FILE=/etc/honey-forge/tls/backend.crt
   TLS_KEY_FILE=/etc/honey-forge/tls/backend.key
   AGENT_WS_URL=wss://api.example.org/assets/stream
   BROWSER_ORIGINS=https://app.example.org
   GIN_MODE=release
   ```

   Замените оба домена своими. `BROWSER_ORIGINS` — фактический HTTPS origin
   фронтенда, с которого браузер вызывает API; это не обязательно домен API.
   Укажите `AGENT_WS_URL` явно, иначе сервер построит его из первого
   `BROWSER_ORIGINS`. Запускайте API с рабочим каталогом `src/backend`;
   `TLS_CERT_FILE` и `TLS_KEY_FILE` должны быть доступны пользователю его
   процесса. Проверьте локальный HTTPS до установки прокси:

   ```sh
   curl --fail --cacert /etc/nginx/certs/backend-ca.crt \
     --resolve localhost:8443:127.0.0.1 https://localhost:8443/healthz
   ```

   Ожидается `{"status":"ok"}`. Ошибка проверки сертификата означает проблему
   с SAN, цепочкой CA или сроком действия; не отключайте проверку TLS.

3. **Установите nginx версии 1.19.4 или новее**: шаблон использует
   `ssl_reject_handshake`. Убедитесь, что основной `nginx.conf` включает
   `/etc/nginx/conf.d/*.conf`. Затем установите и отредактируйте шаблон:

   ```sh
   sudo apt-get update
   sudo apt-get install nginx
   nginx -v
   sudo install -D -m 0644 deploy/nginx/honey-forge.conf /etc/nginx/conf.d/honey-forge.conf
   sudoedit /etc/nginx/conf.d/honey-forge.conf
   sudo nginx -t
   sudo systemctl enable --now nginx
   sudo systemctl reload nginx
   ```

   Выполняйте `install` из корня репозитория. В конфиге замените
   `server_name honeyforge.example` на ваш домен и проверьте пути к публичным
   сертификатам и `backend-ca.crt`. Если `nginx -t` сообщает о повторном
   `default_server` для 443, согласуйте существующие сайты: для этого адреса
   должен остаться один сервер по умолчанию. Не перезагружайте nginx, пока
   `nginx -t` не завершится успешно. Если пакет дистрибутива старее 1.19.4,
   установите поддерживаемую версию nginx перед применением шаблона. При
   замене действующего конфига сначала сохраните его копию вне `conf.d`, чтобы
   при необходимости вернуть её и выполнить `nginx -t` и reload.

4. **Проверьте снаружи и ограничьте доступ.** Откройте на хосте nginx порт 443;
   порт 8443 должен оставаться только на loopback. PostgreSQL и Kafka из
   `compose.yaml` уже привязаны к `127.0.0.1`. Порт 80 нужен лишь если выбранный
   способ выдачи или продления публичного сертификата использует HTTP-01.

   ```sh
   curl -i https://api.example.org/api/session
   curl -i https://api.example.org/assets/stream
   curl -i https://api.example.org/
   ```

   Без cookie первый запрос должен вернуть `401`, без токена агента второй —
   `401`, а `/` — `404`. Затем подключите тестового агента по выданному
   `agent_ws_url` и проверьте, что он стал online: HTTP-ответ `401` сам по себе
   ещё не доказывает успешный WSS Upgrade. Для диагностики используйте
   `sudo journalctl -u nginx -n 100 --no-pager` и журнал Go API. `502` обычно
   означает недоступный `127.0.0.1:8443` или неуспешную проверку его TLS.

При обновлении конфигурации или публичного сертификата снова выполните
`sudo nginx -t` и `sudo systemctl reload nginx`. Настройте автоматическое
продление публичного сертификата; при смене внутреннего CA согласованно обновите
сертификат API и `backend-ca.crt`. Прокси пропускает только `/api/` и
`/assets/stream`; открытые TCP-порты ловушек он не скрывает — их изоляция
настраивается отдельно на хостах агента.

Поведение WSS-прокси и проверки TLS описано в [документации nginx по WebSocket](https://nginx.org/en/docs/http/websocket.html)
и [директивах HTTPS upstream](https://nginx.org/en/docs/http/ngx_http_proxy_module.html).

Go-модуль и единственный API entrypoint находятся в `src/backend/`; полный
контракт — в `api/openapi.yaml`. API подключает auth/organizations, catalog,
profiles, ловушки, команды и приём событий. Состояние ловушек описано в
[модуле traps](src/backend/modules/traps/README.md). Отдельный TCP runtime
агента реализован в `cmd/agent`: [запуск TCP-ловушки](src/backend/internal/agent/README.md).
Отдельное приложение [Redis Medium](src/redis-trap/README.md)
использует тот же контракт управления и доставки телеметрии.
Frontend WSS `/api/stream` подключён: [replay/live, безопасные DTO и reconnect](src/backend/modules/frontendws/README.md).

## 7. Докеризация

`compose.yaml` поднимает только PostgreSQL 17 и Kafka 4.1.2:

```powershell
# Из корня репозитория
docker compose up -d postgres kafka
docker compose ps
```

Порты PostgreSQL `5432` и Kafka `29092` опубликованы только на `127.0.0.1`.
Процесс Go запускается отдельно; Dockerfile для API сейчас нет.

## 8. Ветка Git для продакшена

Основная ветка репозитория — `main` (`origin/main`). Отдельная ветка
`production` не используется. Workflow `.github/workflows/ci-cd.yml` проверяет
PR в `main` и push в `main`: форматирование, зависимости, vet, сборку, unit и
интеграционные тесты с race detector для обоих Go-модулей. PostgreSQL и Kafka
поднимаются как временные сервисы CI; тесты стартуют после их healthcheck.

После успешных проверок push в `main` запускает деплой API на Linux amd64.
Запуски `main` выполняются последовательно; новый push не прерывает текущий
деплой. PR не получает доступ к шагам деплоя.

Перед первым деплоем в GitHub Settings → Secrets and variables → Actions
задайте:

| Настройка | Тип | Значение |
|---|---|---|
| `SSH_HOST` | Secret | Адрес сервера. |
| `SSH_USER` | Secret | Пользователь для загрузки и установки релиза. |
| `SSH_PRIVATE_KEY` | Secret | Закрытый SSH-ключ этого пользователя без passphrase. |
| `SSH_FINGERPRINT` | Secret | SHA256 fingerprint SSH host key, полученный через доверенный доступ к серверу. |
| `SSH_PORT` | Variable | Необязательный порт SSH; по умолчанию `22`. |
| `API_BASE_URL` | Variable | Публичный HTTPS origin API, например `https://api.your-domain.ru`, без пути. |

Fingerprint можно узнать на сервере командой
`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256`; сохраните значение
`SHA256:...` ключа, который использует SSH-сервер. Проверка host key обязательна
для загрузки и выполнения команд. Если обязательная настройка отсутствует,
деплой завершится до подключения к серверу.

На сервере заранее подготовьте PostgreSQL, Kafka, TLS и nginx по разделу 6.
Настроенный nginx устанавливается отдельно: workflow не заменяет домен и
сертификаты шаблоном из Git. Пользователь `SSH_USER` должен иметь право записи
в `/opt/honeyforge` и право без пароля выполнить
`sudo -n systemctl restart honeyforge`. Создайте systemd unit `honeyforge.service`
со следующими путями и окружением из раздела 4, сохранённым вне Git:

```ini
[Service]
WorkingDirectory=/opt/honeyforge/src/backend
ExecStart=/opt/honeyforge/src/backend/honey-forge-api
EnvironmentFile=/etc/honey-forge/api.env
Restart=on-failure
```

Пользователя сервиса, права на environment-файл и TLS-файлы задайте при настройке
хоста. API должен слушать `127.0.0.1:8443`, сертификат иметь SAN `DNS:localhost`,
а CA быть доступен пользователю SSH в `/etc/nginx/certs/backend-ca.crt`.

Workflow загружает бинарник и миграции в уникальный каталог
`/opt/honeyforge/incoming/<run_id>-<attempt>`, устанавливает миграции в
`/opt/honeyforge/migrations` и атомарно заменяет исполняемый файл API перед
перезапуском. Goose применяет миграции при старте приложения. Затем проверяется
локальный `/healthz` с проверкой TLS и публичные ответы `401` для `/api/session`
и `/assets/stream`, `404` для `/`. Публичная проверка выполняется с GitHub runner.
Деплой не обновляет отдельные приложения агентов.

При неуспешном запуске workflow сообщает ошибку; автоматического отката БД и
бинарника нет. Диагностика на сервере: `journalctl -u honeyforge`. Каталоги
`incoming` сохраняются после деплоя; удаляйте старые релизы при обслуживании хоста.

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
