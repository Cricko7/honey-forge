# Catalog

Модуль владеет неизменяемыми версиями типов ловушек, их JSON Schema и
проверкой config, telemetry и command payload. Он предоставляет чтение каталога
через HTTP и проверку схем для модуля profiles.

Общая HTTP-авторизация, пагинация и ошибки берутся из `internal/contract`.
Компиляция безопасных JSON Schema использует `internal/configschema`; записи
версий в PostgreSQL выполняет `repository.go`. Встроенные типы:
`tcp-banner/1` (`schemas/tcp-banner-1.json`) и
`redis-emulator/1` (`schemas/redis-emulator-1.json`). Новую версию следует добавлять
отдельным descriptor, не меняя уже опубликованную.

Маршруты и публичный контракт описаны в [API 03](../../../../api/03-catalog.md)
и [OpenAPI](../../../../api/openapi.yaml). Модуль подключён к общей сборке
`internal/app`; profiles использует его точную версию схемы при создании и
изменении профиля.
