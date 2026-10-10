# Модуль 03: реализация и проверка

Источник требований: [03-catalog.md](03-catalog.md). Реализация:
`src/backend/modules/catalog`; подключение маршрутов: `internal/app/catalog.go`.

**Статус повторной проверки: модуль каталога реализован; полное сквозное
соответствие документу ещё не достигнуто.** Таблица ниже подтверждает локальные
проверки каталога, схем и реальных сессий, а не наличие работающего агента.
В частности, требование «наличие типа означает установленную runtime
реализацию» пока не выполнено: tcp-banner/1 опубликован, но TCP runtime
отсутствует. Реальные сессии модуля 02 и профили 04 подключены и проверены
сквозным PostgreSQL-тестом TestRealSessionCatalogAndProfileFlow.

| Требование | Реализация / проверка |
|---|---|
| CatalogEntry, EventDescriptor, ActionDescriptor, UIHints | `types.go`; declarative validate tags; обязательные start/stop/apply_config, уникальные event/action IDs и JSON Pointer hints проверяются при установке |
| GET списка и точной версии для admin/viewer | `Handler.List/Read`, `Service.List/Read`, `app.RegisterCatalog`; `http_test.go`, `app/catalog_test.go` |
| Неизвестная пара — 404 resource_not_found | Точный ключ `(type_id,type_version)`, без fallback на последнюю версию; `TestLookupType`, `TestHandler` |
| limit/cursor/type_id/available_for_new_profiles | Общий parser limit 1..100/default 50; bool только true/false; неизвестные/повторные/malformed query отвергаются; `TestHandler`, `TestList` |
| Порядок type_id ASC/type_version ASC | Сортировка установленного набора; `TestList`, `TestSnapshotETag` |
| ETag полного каталога; 304 без тела | SHA-256 нормализованных descriptor; private revalidation; ETag одинаков для всех страниц; с cursor If-None-Match не применяется; `TestHandlerConditionalRead` |
| Snapshot, непрозрачный cursor | Sealed snapshot; существующий AES-GCM codec связывает организацию, фильтры и ETag; изменившийся каталог отвергает старый cursor; `TestList`, `TestSnapshotETag` |
| Immutable-версии и сохранение старых | Копии входов/выходов; PostgreSQL catalog_versions; trigger запрещает UPDATE/DELETE; установка изменённой или неполной версии прекращает запуск; `TestSchemaCopies`, `TestRepositoryInstall` |
| JSON Schema 2020-12, локальные $defs/$ref | Каждая config/data/params/result схема самостоятельна; внешний loader выключен; `TestNewService`, `TestDynamicSchemas` |
| object/array/scalars/enum/oneOf, ограничения, formats | Существующий jsonschema compiler с AssertFormat; UUID/IP/date-time/email проверяются сервером; defaults не изменяют config; `TestDynamicSchemas`, `TestCheckConfig` |
| Нет исполняемого UI, произвольных URL или client bind address | UIHints допускает только pointers и описанные widget names; config_schema не допускает дополнительные поля; `TestNewService`, `TestCheckConfig` |
| tcp-banner/1, low, available=true | Embedded `schemas/tcp-banner-1.json`; три tcp event descriptors, три action descriptors; `TestLookupType` |
| Полный config, listeners 1..16, числовые границы | JSON Schema; names/ports уникальны; banner <=4096 UTF-8 bytes; capture false → 0, true → 1..4096; ошибки 422 config_invalid; `TestCheckConfig`, `TestTCPBoundaries` |
| Payload base64/counts/truncated | Канонический base64, decoded length = captured <= original; truncated iff captured < original; `TestCheckEvent` |
| Payload budget на connection, capture disabled | `CheckPayloadCapture` принимает накопленный счётчик и pinned config, возвращает новый счётчик; `TestCheckPayloadCapture` |
| Params/result и revision | `CheckAction`, `CheckActionResult`, `CheckRuntimeResult`; null допустим только stop/stopped до первой конфигурации; `TestCheckAction`, `TestConfiguredRuntimeResult` |
| Расширение event/action без изменения общего API | Схемы выбираются по точной версии/идентификатору; неописанные события отвергаются; `TestDynamicSchemas` |
| Обязательные auth/action event descriptors | Trusted installation flags требуют service.auth_attempt/service.action для поддерживающего runtime; tcp-banner/1 их не объявляет; `TestNewService` |
| Отсутствие operator CRUD | Зарегистрированы только два GET; POST возвращает 405; `TestCatalogAccess` |
| Согласованность OpenAPI | Ответы настоящих Gin handlers проходят схемы CatalogEntry/CatalogPage из OpenAPI; `TestOpenAPIResponse` |
| Параллельные запросы / установка | Неизменяемые maps/schemas; независимые DTO; advisory lock установки и атомарный rollback; `TestConcurrentReads`, `TestRepositoryInstall` |
| Схемы точно соответствуют исходному документу | Сопоставление config/data и локальных action $defs с Markdown источником; `TestBuiltinSchemasMatchSpecification` |
| Числовые integer в динамических схемах | Точное чтение 1/1.0/1e0; дробные значения отвергаются; `TestSchemaIntegerRepresentations` |
| Несколько If-None-Match header values | Учитываются все значения при чтении первой страницы/точной версии; `TestRepeatedConditionalHeaders` |
| Рекурсивные схемы, IPv6, default | Вложенные object/array через $defs; IPv6 проверяется; default не заполняет отсутствующие поля; `TestRecursiveSchemasAndDefaults` |

Реальные сессии admin/viewer подключены: RegisterSessionCatalog проверяет
PostgreSQL-сессию до чтения/304; отзыв сессии проверен сквозным тестом.

## Невыполненные пункты сквозного сценария
- Установленный TCP runtime: отсутствуют открытие listeners, отправка точных
  banner bytes, close_after_banner и 30-секундный idle timeout. Наличие
  descriptor и его валидации не подтверждает эти сетевые действия.
- Выполнение start/stop/apply_config, heartbeat/flush и формирование событий:
  нет агента и модулей команд/телеметрии, которые вызывают границы каталога.
- Межпакетный счётчик captured_bytes и контекст первой конфигурации:
  CheckPayloadCapture/CheckRuntimeResult реализованы, но потребители ещё не
  подключены; поэтому соответствующие инварианты пока проверены только локально.
- catalog.changed: реальная доставка уведомления frontend отсутствует.

## Границы подключения

`app.Open` создаёт `Runtime.Catalog`, сохраняет/сверяет версии после Goose
миграций и регистрирует auth/organizations, profiles и GET-маршруты каталога.
RegisterSessionCatalog проверяет cookie-сессию через настоящий auth service,
затем создаёт contract.Principal. Без активной сессии возвращается 401,
включая запросы с If-None-Match. ProfileTypeLookup использует тот же каталог.
Fake auth остаётся только в изолированных unit-тестах.

Каталог установлен кодом приложения, не пользовательским запросом. При
публикации новой версии нужно добавить новый Definition и оставить прежние.
Снимок не меняется во время работы процесса. После изменения установленного
набора frontend начинает чтение с первой страницы; cursor другого снимка
возвращает 400 invalid_cursor. Доставку catalog.changed реализует модуль 09.

Внутренние `LookupType`, `ConfigSchema`, `CheckConfig`, `CheckAction`,
`CheckActionResult`, `CheckRuntimeResult`, `CheckEvent`, `CheckPayloadCapture`
принимают context; это trusted границы для модулей 04/06/07. Авторизация
публичного чтения остаётся в `Read/List`, остальные потребители авторизуют свои
операции до вызова каталога.

Модуль 04 получает `ConfigSchema` для MergeConfig/PublicConfig и обязательно
вызывает `CheckConfig` на окончательной конфигурации. Модуль 06 вызывает
`CheckRuntimeResult` с фактом предыдущего применения конфигурации из своего
состояния; агент не может сам заявить, что конфигурации ещё не было. Модуль 07
сохраняет результат `CheckPayloadCapture` атомарно с событиями и дедупликацией,
по каждому connection, включая последующие batches. Каталог не хранит
изменяемые счётчики ingestion.

Сетевое исполнение TCP banner (точные UTF-8 bytes без добавления CRLF,
close_after_banner, idle timeout 30 секунд, установка адресов агента), выполнение
команд, heartbeat/flush, доставка телеметрии и auth/action logging принадлежат
агентскому runtime и модулям 06–09. Здесь реализованы их descriptors,
параметры и проверки на границах. Medium-тип `redis-emulator/1` описан в каталоге, а его отдельное приложение
проверяется локальными протокольными тестами без внешнего Redis.

## Проверки

```powershell
Set-Location src/backend
go fmt ./...
go vet ./...
go test ./...
go build ./...
# Отдельная локальная тестовая БД, без production data:
$env:TEST_DATABASE_URL = '<test PostgreSQL connection string>'
go test -tags=integration ./...
# На Windows нужны CGO_ENABLED=1 и доступный C-компилятор:
go test -race -tags=integration ./...
```

SQL-проверка каталога имеет integration build tag. При отсутствии
TEST_DATABASE_URL она явно пропускается. В ходе реализации проверки выполнены
на свежем временном PostgreSQL, включая новую миграцию и защиту версий.
