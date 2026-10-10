# Honeytokens — `honeytoken-http/1`

Реализует FR-A6: раздаёт поддельные ключи, файлы и URL и фиксирует их использование.
Тип добавлен в каталог без изменения существующих версий. Управление идёт через
обычные profiles → traps → apply_config/start/stop. Отдельного хранилища или API
CRUD honeytokens нет: набор приманок принадлежит версии профиля и сохраняется
в PostgreSQL вместе с ним.

## Создание профиля

В UI выберите «HTTP honeytokens» в конструкторе профилей. Шаблон содержит все
три вида приманок и новый случайный ключ. Через API администратор организации
передаёт `POST /api/profiles` с сессией, Origin и X-CSRF-Token:

```json
{
  "request_id": "11111111-1111-4111-8111-111111111111",
  "name": "Backup artifacts",
  "description": "Demonstration only",
  "type_id": "honeytoken-http",
  "type_version": 1,
  "config": {
    "services": [{"name": "web", "port": 8080}],
    "tokens": [
      {"id": "backup-key", "kind": "key", "path": "/v1/backups", "value": "fake-key-0123456789"},
      {"id": "backup-file", "kind": "file", "path": "/backup.txt", "value": "Demo backup: no real data."},
      {"id": "report-url", "kind": "url", "path": "/reports/latest", "value": "Report ready."}
    ],
    "management": {"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 500}
  }
}
```

Одна HTTP-служба, 1–32 приманки. ID и пути уникальны; ключи тоже уникальны.
ID: `[a-z][a-z0-9_-]{0,31}`. Пути абсолютные, до 256 символов, без query,
fragment, percent encoding, повторных `/` и сегментов `.`/`..`.
`/artifacts` и его подпути зарезервированы. Ключ: 16–128 байт без пробелов,
табуляции и переводов строк; остальные значения: 1–4096 символов и максимум
4096 байт. Общие ограничения config также действуют.

`value` — writeOnly: публичные ответы profiles исключают его и возвращают
`secret_fields_set`. Приманки не являются реальными учётными данными и не дают
доступа к внешним сервисам. Снимок для агента содержит полную конфигурацию.
Viewer читает профили и события своей организации, но не создаёт приманки;
чужие ресурсы возвращают 404.

## Раздача и срабатывания

Эти пути обслуживает **порт ловушки**, а не центр управления:

| Запрос | Результат | Срабатывание |
|---|---|---|
| `GET /artifacts/{id}` | JSON `{id, kind, path, value}` для размещения приманки | Нет |
| `GET` к пути key с `Authorization: Bearer <value>` | Фиктивный JSON-ответ | Да |
| key без правильного Bearer | 401 | Нет |
| `GET` к пути file | Текстовый файл с `Content-Disposition: attachment` | Да |
| `GET` к пути url | Заданный текстовый ответ | Да |
| Неизвестный путь / другой метод | 404 / 405 | Нет |

Путь в артефакте относительный к хосту ловушки. Оператор может скопировать
выданную приманку в демонстрационный файл или конфигурацию сервиса. Чтение уже
скачанного файла на другом компьютере не обнаруживается: срабатывает скачивание
с ловушки. DNS, облачные ключи, документы с внешними маячками и реальные внешние
учётные записи не используются. Все ответы имеют `Cache-Control: no-store`.

```sh
curl http://TRAP_HOST:8080/artifacts/backup-key
curl -H 'Authorization: Bearer fake-key-0123456789' http://TRAP_HOST:8080/v1/backups
curl -OJ http://TRAP_HOST:8080/backup.txt
curl http://TRAP_HOST:8080/reports/latest
```

## События

Каждый HTTP-запрос — отдельная логическая сессия: `service.connection_opened`,
при совпадении `honeytoken.triggered`, затем `service.connection_closed`.
`bytes_received` равен 0: тело запроса не читается и не захватывается.
Источник берётся из TCP peer, заголовки Forwarded/X-Forwarded-For игнорируются.

Data срабатывания:

```json
{"service":"web","token_id":"backup-key","kind":"key","method":"GET"}
```

Envelope содержит event_id, session_id/sequence, профиль/revision, время,
адрес/порт источника и порт назначения. Ключ, тело, заголовки и query в событие
не попадают. Центр проверяет ID/вид приманки, службу и порт по **выданному**
снимку профиля. Изменение профиля не переписывает прежние события.

Срабатывание сначала записывается в журнал агента, затем доставляется через
существующий WSS-поток, сохраняется в PostgreSQL и публикуется как
`event.created`. При потере связи действует существующая досылка и дедупликация.
При отказе журнала ловушка прекращает работу вместо выдачи незаписанной приманки.
История: `GET /api/events?event_type=honeytoken.triggered`; детали:
`GET /api/events/{event_id}`. UI выделяет такие события как опасные.

## Развёртывание и проверка

`docker-compose.real.yml` собирает target `honeytokens` из
`deploy/decoys/Dockerfile`. Оркестратор выбирает `DECOY_HONEYTOKEN_IMAGE`
(по умолчанию `honeyforge/honeytoken-decoy:local`) и публикует порт профиля.
Ручной агент также поддерживает `DECOY_TYPE=honeytoken-http`.
Смена набора приманок: PATCH профиля и явный apply_config; stop отключает службу.

Unit-тесты проверяют конфигурацию, скрытие значений, раздачу, три вида
срабатываний, ошибки авторизации ключа, отказ журнала, одновременные обращения,
остановку и восстановление последовательностей. Integration-тест
`TestHoneytokensPersistAndRespectOrganization` проверяет настоящий HTTP runtime,
свежую БД, доставленный снимок, хранение/дедупликацию событий и доступ организаций.
