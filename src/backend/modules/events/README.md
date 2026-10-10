# Приём и REST-история событий атак (модуль 07)

Durable-граница модуля 07:
`Service.Ingest` проверяет AgentEvent и каталог точной версии, фиксирует всю
пачку в PostgreSQL и только затем возвращает ACK. Основной entrypoint подключает
Kafka-журнал через `NewJournalService`: проверяются schema и выданный snapshot,
канонический fingerprint batch фиксируется до публикации. Пачка публикуется с
acks=all, затем материализуется синхронно в PostgreSQL. ACK брокера не является
telemetry.ack. При сбое между Kafka и БД агент повторяет неизменённую пачку;
автономного consumer-процесса эта реализация не требует. Дедупликация event/batch, уникальность sequence внутри
trap/session, исходное received_at и снимки конфигурации сохраняются.

Тип и revision должны относиться к snapshot, ранее выданному этой ловушке.
Для TCP проверяются listener_name, destination port/protocol, literal IP и
occurred_at. Повтор ID с другим содержимым отклоняет всю пачку без частичной
записи. Зарезервированный ingestion завершается вместе с persistence commit;
потерянный ответ безопасно повторяется.

Для TCP payload дополнительно проверяются capture_payload и суммарный лимит
байтов trap/session под блокировкой ловушки. Дедуплицированные события не
списывают бюджет повторно. Captured data хранится в отдельной колонке JSON,
чтобы допустимый NUL не приводил к отказу jsonb; метаданные остаются в jsonb.

GET `/api/events` и `/api/events/{event_id}` доступны admin/viewer своей
организации и продолжают работать после tombstone. Список поддерживает
limit/cursor и фильтры trap_id/from/to/source_ip/event_type/session_id,
сохраняет границу первой страницы и stream_cursor. Детали возвращают исходные
data; summary и технические логи их не копируют. Geo/ASN пока null.

`EventSummary` и `EventPage` экспортируются для frontend stream. Summary целиком
фиксируется в общем журнале как metadata `event.created`, атомарно с событием.
`occurred_at` сохраняет наносекунды; отдельное поле БД дополняет микросекундную
точность PostgreSQL для сортировки, фильтров и курсоров.

Для поддерживающих типов `catalog.StructuredEventSchemas()` предоставляет
service.auth_attempt/service.action. Они не добавлены в tcp-banner/1. Лимиты
username/password (1024) и input (4096) проверяются в UTF-8 байтах; backend не
усекает, не маскирует и не нормализует эти значения. null и пустая строка различны.
Согласованный формат snapshot: `config.services` — массив объектов с `name`;
поле data.service должно точно совпадать с одним из имён. capture_payload=false
не отключает структурированные события. Тестовый service-demo descriptor проверяет
этот путь, но не заявляет наличие работающего Medium-сервиса.

Для запуска API обязательна `KAFKA_BROKERS` (адреса через запятую).
`KAFKA_TELEMETRY_TOPIC` по умолчанию `honey-forge.telemetry`. Локальный broker
поднимается `docker compose up -d kafka`. PostgreSQL остаётся источником истины;
Kafka хранит внутренние записи с захваченными данными, без agent tokens/cookies.
Unit-тесты используют fake publisher; реальные Kafka-проверки запускаются с
`TEST_KAFKA_BROKERS` и `go test -tags=integration ./modules/events`.

Frontend WSS из модуля 09 и отдельный процесс агента не реализуются этим
пакетом. Notifications сохраняются в общем транзакционном журнале для
последующего подключения frontend stream.
