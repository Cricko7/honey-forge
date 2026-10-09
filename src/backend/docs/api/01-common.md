# 01. Общие правила, доступ и ошибки

## Передача модуля

- Нужно: Нет.
- Отдаёт: Общие типы, ошибки, лимиты, Envelope и preconditions. Реальную проверку сессии подключает 02.
- Приёмка: Одинаковая валидация/ошибки и лимиты на разных маршрутах.
- Общий порядок и границы: [README](README.md).

## Адреса и транспорт

Операторский REST: `/api`. WSS фронтенда: `/api/stream`. WSS агента: `/assets/stream` на отдельном управляющем HTTPS-origin. Все подключения только HTTPS/WSS. URL не содержит токенов. Управляющий origin и пути задаются при установке агента; профиль не может переопределить origin или отключить TLS. Нейтральный путь — часть MASK-2, но не гарантия невидимости трафика.

`Content-Type: application/json` обязателен для непустого REST-тела. Ответы с JSON имеют тот же тип. `204` не имеет тела. `201` содержит `Location` существующего GET-ресурса. Первый контракт без `/v1`; параллельная версия появляется только при несовместимом изменении.

Для каждого HTTP-ответа, включая ошибки handshake, сервер выдаёт `X-Request-ID`. REST-ошибка содержит тот же идентификатор в `request_id`. Все REST-ответы: `Cache-Control: no-store`, кроме каталога, где используется `private, max-age=0, must-revalidate` и ETag. Cookies не заменяются токенами в JSON.

## Типы и общая валидация

Все поля в таблицах обязательны, кроме явно помеченных `?`. `T|null` — поле присутствует, но может быть null. `object` — JSON-объект, не массив. JSON использует snake_case, строки UTF-8. Сервер отвергает неизвестные поля оболочек и повторяющиеся имена JSON-полей. Динамический `config`, `data`, `params` проверяется по выбранной схеме каталога, а не по общему Go-типу. Неизвестные поля ответа клиент игнорирует.

| Тип | Ограничения |
|---|---|
| ID | UUID; формат проверяется и в path, и в теле |
| Timestamp | RFC 3339 с часовым поясом; ответы UTC с `Z` |
| Revision | Целое 1..2147483647; переполнение запрещает следующую запись с `409 revision_exhausted` |
| Name | 1..100 символов после удаления внешних пробелов |
| Description | 0..1000 символов; null не разрешён |
| TypeID / Action / EventType | `[a-z][a-z0-9_.-]{0,63}` |
| TypeVersion | Целое 1..2147483647; версия схемы типа, не HTTP API |
| Cursor | Непрозрачная строка до 2048 символов; привязана к организации, виду выборки и фильтрам |
| JSON Schema | JSON Schema Draft 2020-12; inline schema / локальные `$defs`, без загрузки внешних `$ref` |
| Bytes | Base64 стандартного алфавита с padding; пределы указаны в декодированных байтах |

Полный эффективный config (включая сохранённые writeOnly-поля) не более 128 KiB в UTF-8 JSON; это оставляет место оболочке command.dispatch в 256 KiB. Превышение — `422 config_too_large` (`Trap configuration is too large`).

JSON-тело REST не более 256 KiB до binding. Исключений нет: телеметрия поступает по WSS. Превышение — `413 body_too_large`. Пустое обязательное тело, неверный JSON, два JSON-документа, неверный тип поля, неизвестный ключ — `400 invalid_json`. Корректное по структуре тело с отсутствующим обязательным значением / нарушением диапазона — `422 validation_failed`. Неизвестные query-параметры, повтор параметра и неверный UUID/cursor — `400 invalid_query` / `invalid_id` / `invalid_cursor`.

## Изоляция организаций и роли

Сервер получает organization_id из сессии, а для агента — из закреплённой ловушки. Клиент не передаёт organization_id для назначения владельца. Любой чужой ID/cursor возвращает безопасную ошибку без данных чужой организации: чужой ресурс `404 resource_not_found`, cursor `400 invalid_cursor`.

| Возможность | admin | viewer | agent |
|---|---|---|---|
| Читать свою организацию, каталог, профили, ловушки, команды, события, аудит | Да | Да | Нет |
| Создавать/изменять/удалять профили; регистрировать/редактировать/удалять ловушки; команды | Да | Нет | Нет |
| Читать/заменять код вступления; выдавать/отзывать реквизиты агента | Да | Нет | Нет |
| WSS фронтенда своей организации | Да | Да | Нет |
| WSS агента: своя конфигурация, команды, heartbeat, события | Нет | Нет | Да |

Viewer видит захваченные атакующие данные в событиях, но не серверные секреты, код организации и реквизиты агента. Клиентские пароли оператора, cookies и agent tokens не возвращаются в профиль, события или аудит. Будущие настоящие секреты типа имеют `writeOnly: true`: возвращаются как список установленных JSON Pointer в `secret_fields_set`, значение отсутствует. Изменение config с пропущенным установленным writeOnly-полем сохраняет его; для сброса используется отдельный массив `clear_secret_fields`. Он содержит только writeOnly-пути текущей схемы. Креды-приманки не являются секретами оператора; их видимость задаёт схема типа.

Проверка доступа выполняется до поиска чужого ресурса и до побочного эффекта: аутентификация, Origin/CSRF для изменяющего browser-запроса, роль, принадлежность ресурса, проверка входа и preconditions. Неаутентифицированный запрос не определяет наличие ресурса по различию 403/404.

## Изменения и конкурентные запросы

GET изменяет только технические заголовки/cookies, но не бизнес-состояние. Profile содержит `revision`; GET и успешная запись выдают ETag `"profile:<id>:<revision>"`. PATCH/DELETE Profile требуют точный `If-Match`; отсутствующий — `428 precondition_required`, несовпадающий — `412 revision_mismatch`. `If-Match: *` не принимается: `400 invalid_precondition`. Проверка версии и запись атомарны.

Trap использует `revision` редактируемых полей и `state_version` полного DTO. PATCH/DELETE Trap требуют `X-Expected-Revision: <revision>` из GET Trap. Отсутствующий заголовок — `428 precondition_required`, неверное целое/повтор заголовка — `400 invalid_precondition`, устаревшая revision — `412 revision_mismatch`. Heartbeat/команды не меняют revision. ETag полной Trap не выдаётся; If-Match не заменяет X-Expected-Revision. Проверка revision, условий жизненного цикла и запись атомарны.

POST создания Profile/Trap/Command содержит `request_id: ID`: повтор в той же организации, на том же маршруте, с тем же телом возвращает существующий ресурс `200`; первый — `201`. После удаления повтор даёт `409 request_already_used`. Другое тело с тем же ключом — `409 idempotency_conflict`. Ключи сохраняются весь срок жизни организации; нормализованные значения сравниваются, порядок JSON-ключей не влияет. Для Command область ключа — ловушка. Профили/ловушки могут иметь одинаковые имена: имя не идентификатор.

## Пагинация и время

Все списки: `limit` 1..100, default 50; `cursor` optional. Ответ `Page<T> = {items: T[], next_cursor: Cursor|null}`. Пустой список: `items: [], next_cursor: null`. Cursor не переносится между фильтрами. Последующие страницы сохраняют границу выборки первой страницы: новые записи не сдвигают старые страницы. Удалённая запись может исчезнуть. Сортировка фиксирована конкретным модулем; произвольный `sort` не поддерживается.

`from` включительно, `to` исключительно; `from < to`, иначе `422 invalid_time_range`. Время фильтрации событий — occurred_at; время появления в системе — received_at. Сервер не переписывает исходное время события. Для списков без времени эти параметры не принимаются.

## ErrorResponse и Error

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Request validation failed",
    "fields": [{"path": "/config/listeners/0/port", "code": "out_of_range", "message": "Value is outside the allowed range"}]
  },
  "request_id": "11111111-1111-4111-8111-111111111111"
}
```

`Error`: `code: string`, `message: string`, `fields?: FieldError[]`. `FieldError`: `path: string` (JSON Pointer; query использует `/query/name`), `code: string`, `message: string`. `ErrorResponse`: `error: Error`, `request_id: ID`. Значения секретов/паролей/payload не отражаются в ошибках. `fields` выдаётся для 400/422 и не более 20 элементов. Клиент ветвится по code, а не message. Публичные message из таблицы фиксированы; подробности только в fields безопасным текстом.

| HTTP | code | message |
|---|---|---|
| 400 | invalid_json | Invalid JSON body |
| 400 | invalid_query | Invalid query parameters |
| 400 | invalid_id | Invalid resource identifier |
| 400 | invalid_cursor | Invalid pagination cursor |
| 400 | invalid_precondition | Invalid precondition header |
| 401 | unauthenticated | Authentication required |
| 401 | invalid_credentials | Invalid email or password |
| 401 | agent_unauthenticated | Invalid agent credentials |
| 403 | forbidden | Insufficient permissions |
| 403 | csrf_failed | CSRF validation failed |
| 403 | origin_not_allowed | Origin is not allowed |
| 404 | resource_not_found | Resource not found |
| 405 | method_not_allowed | Method not allowed |
| 409 | idempotency_conflict | Request identifier was used with different content |
| 409 | request_already_used | Request identifier belongs to a deleted resource |
| 409 | revision_exhausted | Resource revision limit reached |
| 412 | revision_mismatch | Resource has changed |
| 413 | body_too_large | Request body is too large |
| 415 | unsupported_media_type | Expected application/json |
| 422 | validation_failed | Request validation failed |
| 422 | invalid_time_range | Invalid time range |
| 428 | precondition_required | A precondition header is required |
| 429 | rate_limited | Too many requests |
| 500 | internal_error | Internal server error |
| 503 | database_unavailable | Database is temporarily unavailable |
| 503 | service_unavailable | Service is temporarily unavailable |

Модульные коды дополняют эту таблицу. Базовые ошибки применяются к каждому маршруту без повторения. `429` и retryable `503` содержат `Retry-After` в секундах. Повторять POST можно только с прежним request_id, где он предусмотрен. Ошибка/таймаут не означает отсутствие побочного эффекта: сначала повтор/чтение ресурса.

## Совместимость и расширяемость

Новые TypeID, TypeVersion, Action и EventType — данные каталога, не новые endpoint/поля общего контракта. Изменённая схема под старым type_id/type_version запрещена. Читатель неизвестного типа показывает title и безопасный JSON, но не выполняет содержимое как код. Добавление runtime реализации типа всё равно необходимо; каталог не создаёт работающий сервис автоматически.

Нормативные основы схем: [JSON Schema Validation](https://json-schema.org/draft/2020-12/json-schema-validation), условных запросов и HTTP-кодов: [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html). Значения лимитов и интервалы здесь — выбранные параметры предлагаемого контракта, а не числа из PDF.

## Общая оболочка WSS

Оба WSS используют Envelope: `message_id: ID`, `type: string` (1..64), `reply_to?: ID|null`, `payload: object`. Клиентский запрос: reply_to отсутствует/null; ответ сервера: reply_to равен message_id запроса; уведомление: reply_to=null. Корреляция не заменяет дедупликацию event_id/command_id/batch_id. UTF-8 JSON text, максимум 256 KiB на frame и итоговое сообщение, compression отключена. Binary — close 1003, превышение размера — 1009. Конкретные payload определяются в своих модулях.

## Границы командной сборки

Защищённые модули получают от авторизации серверные user_id, organization_id и role; CSRF проверяется до записи. Общие ошибки, ID, лимиты, Page и Envelope определяются только здесь. Введённые атакующим логины/пароли разрешены в Event.data своей организации; это не пароли оператора. В технические логи, ошибки и audit они не копируются.

Бизнес-запись, её обязательный audit и воспроизводимое stream-уведомление фиксируются согласованно: нельзя успешно ответить, потеряв обязательную запись. Владелец feature задаёт изменение и безопасные metadata; общий журнал изменений нужен REST stream_cursor уже до подключения frontend WSS. Поздние модули подключают чтение/доставку, не повторяют бизнес-операцию. До подключения зависимостей изолированные проверки допустимы, но сквозная сборка не объявляется завершённой.
