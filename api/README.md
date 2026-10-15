# API HoneyForge

[OpenAPI](openapi.yaml) описывает HTTP-маршруты, схемы запросов и ответов.
Тест `TestOpenAPIContainsEveryRouteAndResponseReference` сверяет контракт с роутером.

Реализация находится в `src/backend/modules`, общая сборка — в `src/backend/internal/app`.

| Модуль | Назначение |
|---|---|
| auth | Cookie-сессии, API-ключи, организации и права доступа |
| catalog | Версии типов ловушек и JSON Schema |
| profiles | Конфигурации, ревизии и секретные поля |
| traps | Регистрации ловушек, реквизиты агентов и состояние |
| commands | apply_config, start, stop и история команд |
| events | Приём и история событий атак |
| agentws | Управление агентами и доставка телеметрии по WSS |
| frontendws | Поток изменений для панели |
| audit | История действий операторов |

[Запуск и проверки](../README.md) · [Описание модулей](../src/backend/README.md).
