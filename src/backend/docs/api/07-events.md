# 07. События атак: приём и REST-чтение

## Передача модуля

- Нужно: 01–06; транспорт поступает из 08, уведомления доставляются 09.
- Отдаёт: AgentEvent/Event, атомарный ingest, REST-фильтры, TelemetryBatch/Ack и stream_cursor.
- Приёмка: Повтор/конфликт, чужой trap, история, исходные username/password/input.
- Общий порядок и границы: [README](README.md).

## Общая схема событий

| Схема | Поля |
|---|---|
| NetworkSource | `ip: string` (IPv4/IPv6 literal, без zone/hostname), `port: integer` (1..65535) |
| NetworkDestination | `protocol: string` (1..32), `port: integer` (1..65535) |
| AgentEvent | `event_id: ID`, `event_type: EventType`, `type_id: TypeID`, `type_version: TypeVersion`, `profile_revision: Revision`, `occurred_at: Timestamp`, `session_id: ID`, `session_sequence: integer` (1..9007199254740991), `source: NetworkSource`, `destination: NetworkDestination`, `data: object` |
| EventSummary | все поля AgentEvent кроме data, плюс `trap_id: ID`, `received_at: Timestamp` |
| Event | EventSummary плюс `data: object`, `source_enrichment: SourceEnrichment` |
| SourceEnrichment | `country_code: string|null` (ISO 3166-1 alpha-2), `asn: integer|null` (1..4294967295) |
| EventPage | `items: EventSummary[]`, `next_cursor: Cursor|null`, `stream_cursor: Cursor` |

AgentEvent до добавления backend-полей не более 16 KiB в UTF-8 JSON. event_type и data проверяются по event_schemas конкретного типа/версии. type_id/type_version должны совпадать с типом Trap; profile_revision — snapshot, ранее выданный этой ловушке. При задержке/смене текущей версии Profile принимаются старые события с ранее выданной revision. Неизвестная/чужая revision, некорректный source или неподдерживаемый event_type — telemetry_invalid. IP канонизируется сервером при хранении/сравнении, hostname не принимается.

Для tcp-banner destination.protocol="tcp", port и data.listener_name должны соответствовать заявленному snapshot. occurred_at не позднее server_time+5 минут; нижний предел отсутствует ради досылки после длительного offline. received_at — серверная метка первого принятия к сохранению, зафиксированная вместе с первым commit; повторы её не меняют. Неизвестные geo/ASN возвращаются null, API не обещает наличие обогащения в первом Low-сценарии.

session_id группирует одно взаимодействие сервиса (TCP-подключение либо сессию поддерживающего типа); sequence отражает порядок генерации внутри этой сессии. Агент генерирует последовательность с 1 и сохраняет её с событием. Досылка может приходить не по порядку; отдельный event не требует наличия connection_opened. Отсутствующие события/незавершённая сессия не подменяются вымышленными данными. Повтор sequence с другим event_id в той же trap/session — telemetry_invalid; одинаковое повторное событие подтверждается. Дедупликация `(trap_id,event_id)` не сбрасывается рестартом, reconnect или сменой credentials. Каноническое содержимое включает все AgentEvent-поля; нормализация IP/time и порядок ключей не создают ложный конфликт.

Для tcp-banner отсутствуют auth attempts/реальные команды ОС: он фиксирует подключения и TCP-данные. Сервисы с аутентификацией/командами объявляют service.auth_attempt/service.action по схемам ниже; общий Event не изменяется. Бинарные данные находятся в base64 внутри data; клиент не трактует bytes как доверенный HTML/JS. Захваченные credentials являются данными атакующего, не credentials центра. Никакие поля data не исполняются frontend автоматически.

```json
{
  "event_id":"33333333-3333-4333-8333-333333333333",
  "event_type":"tcp.connection_opened",
  "type_id":"tcp-banner",
  "type_version":1,
  "profile_revision":1,
  "occurred_at":"2026-10-09T12:00:00Z",
  "session_id":"44444444-4444-4444-8444-444444444444",
  "session_sequence":1,
  "source":{"ip":"192.0.2.10","port":50123},
  "destination":{"protocol":"tcp","port":2222},
  "data":{"listener_name":"ssh"}
}
```

## GET /api/events

Admin/viewer. Query limit/cursor, `trap_id?`, `from?`, `to?`, `source_ip?` (IP literal, канонизация), `event_type?`, `session_id?`. `200 EventPage`. Порядок occurred_at DESC/event_id DESC. Все фильтры соединяются AND. Неизвестный валидный event_type или session_id даёт пустую Page; удалённый свой trap_id читается по tombstone; чужой/неизвестный trap_id — 404, не подсказка о чужих событиях.

На первой странице фиксируется snapshot и возвращается соответствующий stream_cursor всей организации. Следующие страницы содержат тот же stream_cursor и тот же snapshot. Он используется для WSS replay, а next_cursor — только для REST-пагинации; взаимозаменяемость отсутствует. Новые запоздалые события с ранним occurred_at не вставляются в продолжаемую REST-выборку; появляются после обновления первой страницы и через event.created. Пустая Page также содержит действительный stream_cursor.

## GET /api/events/{event_id}

Admin/viewer. `200 Event`. Внешние event_id глобально уникальны для адресации чтения: если агент повторно использует event_id другой ловушки, batch получает event_id_conflict, без раскрытия владельца. Это дополняет ключ дедупликации trap/event. Чужой event — 404. События нельзя менять/удалять через этот API. Запись сохраняет оригинальную версию типа и config snapshot; последующее редактирование/удаление Profile не меняет data.

## Обязательная цепочка ловушка → агент → backend → frontend

1. Ловушка передаёт агенту фактическое взаимодействие: подключение, введённые логин/пароль, команду либо запрос. Агент назначает устойчивые event_id/session_id/session_sequence и исходное occurred_at, фиксирует AgentEvent локально до продолжения взаимодействия.
2. Агент отправляет telemetry.batch (модуль 08), включая досылку. Backend валидирует каталог и сохраняет Event.data в PostgreSQL до telemetry.ack. Захваченные username/password и input не заменяются масками на API-границе.
3. Backend отправляет frontend event.created (модуль 09) с EventSummary. Frontend запрашивает GET /api/events/{event_id} и получает полный Event.data с логином, паролем или действием. REST-чтение деталей — обязательная часть отображения; WSS summary не содержит data. Admin/viewer видят только свою организацию. HTML/JS/команды показываются как текст и не исполняются.
4. История сохраняется после stop/start и удаления ловушки. Пароли оператора, cookies, agent tokens не копируются в эти события. Технические логи, ошибки и audit не содержат захваченных паролей и input.

## Схемы data структурированных событий сервиса

Схемы публикуются в event_schemas только поддерживающих их типов. Они не расширяют tcp-banner/1 и не заменяют реализацию Medium. Сетевые поля, profile_revision и session_id/sequence находятся в общей оболочке AgentEvent.

### service.auth_attempt

```json
{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["service","username","password","outcome","truncated"],"properties":{"service":{"type":"string","minLength":1,"maxLength":64},"username":{"type":["string","null"],"maxLength":1024},"password":{"type":["string","null"],"maxLength":1024},"outcome":{"type":"string","enum":["accepted","rejected","unknown"]},"truncated":{"type":"boolean"}}}
```

username/password — введённые атакующим значения без trim/lowercase/маскирования; null — значение не вводилось, пустая строка — введено пустое значение. Каждый текст максимум 1024 UTF-8 байт; превышение фиксируется префиксом по границе UTF-8 с truncated=true. false означает отсутствие усечения. outcome описывает ответ эмулятора, а не вход в центр. Неуспешные попытки тоже записываются.

### service.action

```json
{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["service","action_kind","input","outcome","truncated"],"properties":{"service":{"type":"string","minLength":1,"maxLength":64},"action_kind":{"type":"string","enum":["command","request"]},"input":{"type":"string","maxLength":4096},"outcome":{"type":"string","enum":["accepted","rejected","unknown"]},"truncated":{"type":"boolean"}}}
```

input — исходная текстовая команда или запрос; максимум 4096 UTF-8 байт, усечение только с truncated=true. Для HTTP request содержит метод, request-target и текстовое тело, если есть; бинарные payload — отдельное событие соответствующего типа. action_kind=command описывает ввод атакующего, а не action start/stop/apply_config оператора. accepted означает ответ эмулятора, выполнение команды ОС не подразумевается. service (1..64 символов) соответствует сервису snapshot.

Примеры AgentEvent.data:

```json
{"service":"ssh","username":"root","password":"attacker-password","outcome":"rejected","truncated":false}
```

```json
{"service":"ssh","action_kind":"command","input":"whoami","outcome":"accepted","truncated":false}
```

Лимит всего AgentEvent 16 KiB имеет приоритет над лимитами полей. Producer учитывает JSON escaping и при необходимости сокращает текст с truncated=true до локальной фиксации, а не при retry. capture_payload=false не отключает структурированные auth/action.

## TelemetryBatch / TelemetryAck

Агент → `telemetry.batch`: `{batch_id: ID, events: AgentEvent[]}`; 1..100 событий, один неподтверждённый batch на соединение. AgentEvent и правила версий определены выше. Транспорт/аутентификация соединения — в модуле 08. batch_id закрепляет неизменяемый набор и порядок event_id. message_id при retry новый; batch_id и содержимое прежние. Одинаковый batch_id с другим содержимым — Error batch_conflict. Повтор event_id внутри batch — Error validation_failed. Предел frame важнее max_batch_events: агент делит пачку заранее.

Backend → `telemetry.ack`: `{batch_id: ID, acknowledged_event_ids: ID[], stored_at: Timestamp}`. Список содержит все ID исходной пачки в том же порядке; повторно сохранённые события также подтверждаются. Подтверждение означает: каждый event_id существует в PostgreSQL с тем же каноническим содержимым. Оно не означает «только положили в Kafka» и не зависит от подключения фронтенда.

В центре используется Kafka; детали topics, partitions и SQL не являются публичным API. Гарантия на границе: at-least-once доставка с одной записью/эффектом на `(trap_id,event_id)`. Продюсерский ack Kafka не заменяет проверку сохранения в PostgreSQL. Для external storage граница exactly-once требует участия хранилища: [Apache Kafka: delivery semantics](https://kafka.apache.org/41/design/design/#message-delivery-semantics). Контракт фиксирует эффект в БД, а не обещает единственный сетевой пакет.

Backend проверяет schema всей пачки перед принятием, сохраняет её события атомарно при материализации. Нет частичного положительного ack. При окончательной schema-ошибке агент сохраняет непринятые события отдельно от отправляемых, выставляет last_error=telemetry_invalid; корректные события может пересобрать в новые batch_id, сохраняя event_id. Ошибочные события не удаляются и не исправляются под прежним event_id после принятия центром. Это не позволяет одному неверному событию блокировать все корректные до заполнения буфера. ID с другим содержимым — Error event_id_conflict; остальные элементы конфликтной пачки не подтверждаются. Уже существующие одинаковые записи не меняются. received_at первого сохранения остаётся исходным. Ошибка/разрыв после commit приводит к retry и тому же ack без дубликатов.

Если за 10 секунд сохранение не подтверждено, сервер отвечает Error ingestion_pending с retry_after_seconds=2. Сообщение могло уже попасть в Kafka или БД: агент не удаляет batch, повторяет тот же batch_id/event_id. Недоступность Kafka: telemetry_unavailable; PostgreSQL: database_unavailable. Неполученный ответ за 12 секунд — retry с прежним batch, максимум один выполняемый batch; server busy — Error ingestion_busy. Сервер сопоставляет подтверждение с batch и активным соединением, не отправляет чужой ack.
