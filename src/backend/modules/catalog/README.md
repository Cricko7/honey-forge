# Catalog

Модуль владеет неизменяемыми версиями типов ловушек, их JSON Schema и
проверкой config, telemetry и command payload. Он предоставляет чтение каталога
через HTTP и проверку схем для модуля profiles.

Общая HTTP-авторизация, пагинация и ошибки берутся из `internal/contract`.
Компиляция безопасных JSON Schema использует `internal/configschema`; записи
версий в PostgreSQL выполняет `repository.go`. Встроенные типы:
`tcp-banner/1` (`schemas/tcp-banner-1.json`) и
`redis-emulator/1` (`schemas/redis-emulator-1.json`) и
`honeytoken-http/1` (`schemas/honeytoken-http-1.json`) и
`ssh-high-interaction/1` (`schemas/ssh-high-interaction-1.json`) и
`ssh-high-interaction/2` (`schemas/ssh-high-interaction-2.json`, host-side
Tetragon `process.exec`; требует установленного оператором сенсора).
Новую версию следует добавлять
отдельным descriptor, не меняя уже опубликованную.

При запуске с существующей базой допускается также точный ранний pipeline-дескриптор
`custom-http/1`, опубликованный до выделения версии 2. API сохраняет его из базы
без изменения; runtime поддерживает соответствующие старые snapshots. На новой
базе устанавливаются обычные версии 1 и 2. Другие несовпадения дескрипторов по-прежнему
останавливают запуск; совместимость не отключает проверку неизменяемости.

Маршруты и публичный контракт описаны в [OpenAPI](../../../../api/openapi.yaml). Модуль подключён к общей сборке
`internal/app`; profiles использует его точную версию схемы при создании и
изменении профиля.
