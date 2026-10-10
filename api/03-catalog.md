# 03. Каталог типов и динамические схемы

## Передача модуля

- Нужно: 01–02.
- Отдаёт: CatalogEntry и immutable-схемы для 04/06/07.
- Приёмка: Чтение admin/viewer; неизвестная версия; config/event/action schemas.
- Общий порядок и границы: [README](README.md).

## Универсальная модель

Профиль выбирает точную пару `(type_id, type_version)` и содержит `config`. Общая схема Profile не содержит вариантов tcp/http/ssh и не требует изменения при добавлении типа. Каталог описывает настройки, события, действия и форму. Новая версия типа публикуется отдельной записью; старые версии остаются для существующих профилей и отложенной телеметрии.

| Схема | Поля |
|---|---|
| CatalogEntry | `type_id: TypeID`, `type_version: TypeVersion`, `title: string` (1..100), `description: Description`, `interaction_level: string` (1..32), `available_for_new_profiles: boolean`, `config_schema: JSON Schema`, `event_schemas: EventDescriptor[]`, `actions: ActionDescriptor[]`, `ui: UIHints` |
| EventDescriptor | `event_type: EventType`, `title: string` (1..100), `data_schema: JSON Schema` |
| ActionDescriptor | `action: Action`, `title: string` (1..100), `params_schema: JSON Schema`, `result_schema: JSON Schema` |
| UIHints | `field_order: string[]` (JSON Pointer), `widgets: object` (JSON Pointer -> `text`, `textarea`, `number`, `checkbox`, `select`, `password`, `array`, `object`) |

`interaction_level` не является закрытым enum общего API: сейчас low и medium, позже high. `event_schemas` и `actions` содержат уникальные идентификаторы. Для типа обязательны базовые actions start, stop, apply_config. Дополнительные actions объявляются каталогом без изменения маршрутов. Поле default в JSON Schema — подсказка, не автоматическое изменение запроса; клиент отправляет полную конфигурацию.

Форма использует рекурсивно object/array, scalar, enum, oneOf и `$defs`, ограничения и title/description/default. UIHints — необязательные для клиента подсказки: неизвестный widget пропускается, применяется универсальный редактор JSON. Сервер валидирует независимо от формы. `format` проверяется сервером для используемых форматов (UUID/IP/date-time/email), а не только отображается. Конфигурация типов не может содержать исполняемые JS, URL для загрузки формы или произвольный код UI.

## GET /api/trap-types

Admin/viewer. Query `limit`, `cursor`, `type_id?`, `available_for_new_profiles?` (`true|false`). `200 Page<CatalogEntry>`. Порядок type_id ASC, type_version ASC. Для первой страницы разрешён `If-None-Match` ETag текущего полного каталога; совпадение — `304` без тела. Для страниц с cursor условное чтение не применяется. ETag всех страниц одинаковый для одного snapshot. При catalog.changed frontend заново получает первую страницу и последующие страницы этого snapshot.

## GET /api/trap-types/{type_id}/versions/{type_version}

Admin/viewer. `200 CatalogEntry`, ETag; If-None-Match может вернуть `304`. Неизвестная пара — `404 resource_not_found`. Список не содержит секретов и настроек конкретного профиля. CRUD каталога через operator API отсутствует: наличие типа означает установленную и поддерживаемую runtime реализацию.

## Первый тип tcp-banner / 1

`interaction_level=low`, `available_for_new_profiles=true`. Конфигурация позволяет несколько TCP listeners в одной ловушке. Рабочие параметры задаются полностью:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["listeners", "logging", "management"],
  "properties": {
    "listeners": {
      "type": "array", "minItems": 1, "maxItems": 16,
      "items": {
        "type": "object", "additionalProperties": false,
        "required": ["name", "port", "banner", "close_after_banner"],
        "properties": {
          "name": {"type": "string", "pattern": "^[a-z][a-z0-9_-]{0,31}$"},
          "port": {"type": "integer", "minimum": 1, "maximum": 65535},
          "banner": {"type": "string", "minLength": 1, "maxLength": 4096},
          "close_after_banner": {"type": "boolean"}
        }
      }
    },
    "logging": {
      "type": "object", "additionalProperties": false,
      "required": ["capture_payload", "max_payload_bytes"],
      "properties": {
        "capture_payload": {"type": "boolean"},
        "max_payload_bytes": {"type": "integer", "minimum": 0, "maximum": 4096}
      }
    },
    "management": {
      "type": "object", "additionalProperties": false,
      "required": ["heartbeat_interval_seconds", "telemetry_flush_interval_ms"],
      "properties": {
        "heartbeat_interval_seconds": {"type": "integer", "minimum": 5, "maximum": 10},
        "telemetry_flush_interval_ms": {"type": "integer", "minimum": 100, "maximum": 1000}
      }
    }
  }
}
```

Дополнительные доменные ограничения: listener names и ports уникальны внутри config; banner максимум 4096 UTF-8 байт; `capture_payload=false` требует `max_payload_bytes=0`, true требует 1..4096. Нарушение — `422 config_invalid`. Сетевой адрес привязки и mgmt-интерфейс определяются установкой агента и не являются произвольными клиентскими адресами. Banner отправляется как точные UTF-8 байты без автоматически добавляемого CRLF. close_after_banner=false допускает получение данных до закрытия клиентом или 30 секунд простоя; это фиксированное поведение tcp-banner/1. Ловушка не выполняет команды ОС и не заявляет успешную аутентификацию: это Low.

### Схема data для tcp.connection_opened

```json
{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["listener_name"],"properties":{"listener_name":{"type":"string","pattern":"^[a-z][a-z0-9_-]{0,31}$"}}}
```

### Схема data для tcp.payload_received

```json
{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["listener_name","payload_base64","captured_bytes","original_bytes","truncated"],"properties":{"listener_name":{"type":"string","pattern":"^[a-z][a-z0-9_-]{0,31}$"},"payload_base64":{"type":"string","maxLength":5464,"contentEncoding":"base64"},"captured_bytes":{"type":"integer","minimum":0,"maximum":4096},"original_bytes":{"type":"integer","minimum":1},"truncated":{"type":"boolean"}}}
```

Base64 обязан декодироваться; decoded length = captured_bytes <= original_bytes. truncated = captured_bytes < original_bytes. Сумма captured_bytes payload-событий одного connection ограничена max_payload_bytes; при capture_payload=false payload-события не формируются. original_bytes — размер соответствующего прочитанного фрагмента, не суммарный размер подключения.

### Схема data для tcp.connection_closed

```json
{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["listener_name","duration_ms","bytes_received","reason"],"properties":{"listener_name":{"type":"string","pattern":"^[a-z][a-z0-9_-]{0,31}$"},"duration_ms":{"type":"integer","minimum":0},"bytes_received":{"type":"integer","minimum":0},"reason":{"type":"string","enum":["peer_closed","banner_sent","idle_timeout","service_stopped","network_error"]}}}
```

### Actions для tcp-banner/1

| action | params_schema | result_schema |
|---|---|---|
| start | EmptyObject | RuntimeResult |
| stop | EmptyObject | RuntimeResult |
| apply_config | ApplyConfigParams | RuntimeResult |

```json
{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"EmptyObject":{"type":"object","additionalProperties":false,"maxProperties":0},"ApplyConfigParams":{"type":"object","additionalProperties":false,"required":["profile_revision"],"properties":{"profile_revision":{"type":"integer","minimum":1,"maximum":2147483647}}},"RuntimeResult":{"type":"object","additionalProperties":false,"required":["runtime_state","applied_profile_revision"],"properties":{"runtime_state":{"type":"string","enum":["running","stopped"]},"applied_profile_revision":{"type":["integer","null"],"minimum":1,"maximum":2147483647}}}}}
```

В RuntimeResult null разрешён только для stop до первой конфигурации; start/apply_config требуют целую revision.

Для публикации descriptor каждая схема должна иметь собственный `$schema` и необходимые `$defs` внутри себя. Документ с общими `$defs` выше компактно определяет три схемы; внешние ссылки не требуются. Учётные данные-приманки у TCP-баннера отсутствуют; тип, который поддерживает их, определяет поля в своём config_schema. Аналогично новые протоколы, payload и команды описываются в каталоге; общие Profile/Event/Command не меняются.

## Логирование сервисов с аутентификацией и действиями

Тип с аутентификацией обязан объявить service.auth_attempt в event_schemas; тип с командами/запросами — service.action. Общие data_schema определены в модуле 07. Backend не принимает событие, не объявленное точной версией типа. Новые схемы публикуются под новой immutable-версией, старые не меняются. Логирование auth/action обязательно для поддерживающего типа и не отключается capture_payload, управляющим сырыми payload.

tcp-banner/1 остаётся Low: не извлекает пароли/команды из произвольных TCP-байтов и не объявляет auth/action.

## Medium-тип redis-emulator / 1

`interaction_level=medium`, `available_for_new_profiles=true`. Полная схема
находится в `src/backend/modules/catalog/schemas/redis-emulator-1.json`.
Конфигурация содержит ровно один элемент `services` с `name`, `port` (1..65535)
и `password` (1..128 символов, `writeOnly`), а также те же интервалы
`management`, что у TCP-баннера. Создание профиля, регистрация Trap, реквизиты
агента и команды `apply_config`, `start`, `stop` используют те же маршруты и
формы запросов. Пароль хранится в полном snapshot для ловушки, но скрывается в
ответах профиля.

Тип объявляет `service.connection_opened`, `service.auth_attempt`,
`service.action`, `service.connection_closed`. Auth/action включают исходные
введённые значения, outcome, признак усечения и `received_bytes`; закрытие
содержит длительность, суммарные байты и причину. Для каждого события backend
проверяет service name и destination port по выданному snapshot. Сырые RESP
кадры целиком не сохраняются: действия нормализуются в текст команды с
ограничением 4096 UTF-8 байт. Протокол и демонстрационный сценарий описаны в
`src/backend/internal/decoys/redistrap/README.md`.
