# Приём и история событий для жизненного цикла ловушки

Минимальная durable-граница модуля 07, необходимая для `05-traps.md`:
`Service.Ingest` проверяет AgentEvent и каталог точной версии, фиксирует всю
пачку в PostgreSQL и только затем возвращает ACK. Очередь сообщений для этой
реализации не требуется. Дедупликация event/batch, уникальность sequence внутри
trap/session, исходное received_at и снимки конфигурации сохраняются.

Тип и revision должны относиться к snapshot, ранее выданному этой ловушке.
Для TCP проверяются listener_name, destination port/protocol, literal IP и
occurred_at. Повтор ID с другим содержимым отклоняет всю пачку без частичной
записи. Зарезервированный ingestion завершается вместе с persistence commit;
потерянный ответ безопасно повторяется.

GET `/api/events` и `/api/events/{event_id}` доступны admin/viewer своей
организации и продолжают работать после tombstone. Список поддерживает
limit/cursor и фильтры trap_id/from/to/source_ip/event_type/session_id,
сохраняет границу первой страницы и stream_cursor. Детали возвращают исходные
data; summary и технические логи их не копируют. Geo/ASN пока null.

Frontend WSS из модуля 09 и отдельный процесс агента не реализуются этим
пакетом. Notifications сохраняются в общем транзакционном журнале для
последующего подключения frontend stream.
