# Совместимость common, catalog и backend из origin/main

Auth/organizations и profiles из `origin/main` (49fe686) включены в общий
checkout. Исходные незакоммиченные файлы предварительно сохранены в резервной
копии. Git merge/push не выполнялись.

- Единственный Go-модуль расположен в `src/backend`; весь код входит в `go test ./...`.
- Миграции auth, profiles, common и catalog находятся в корневом `migrations/`.
  Goose применяет также ещё не установленные миграции с более ранним timestamp,
  поэтому база с уже установленным common может получить auth/profiles.
- Единственный entrypoint использует `internal/app.Open` для реальных сервисов,
  маршрутов и установленного каталога. Запуск из `src/backend`: `go run ./cmd/api` (TLS).
- `RegisterSessionCatalog` проверяет настоящую cookie-сессию и создаёт
  `contract.Principal` из серверного UserID, OrganizationID и Role. Проверка
  выполняется до чтения/304; отозванная сессия получает 401.
- Profiles получает `ProfileTypeLookup(Runtime.Catalog)`. Отдельная TCP-схема и
  ручной валидатор соседней ветки убраны. HTTP-каталог и проверка профиля
  используют одинаковую immutable-версию, включая целые 2222.0 и 2.222e3.
- Сохранение пропущенных writeOnly-полей, очистка и перечисление секретов
  используют общие `Schema.MergeConfig/PublicConfig`. Полный эффективный
  config дополнительно проверяется каталогом; данные ответа скрывают секреты.
- Origin/CSRF, роли, лимит тела, request ID и недоверенные proxy headers
  проверяются в общей сборке. Единственный OpenAPI — `api/openapi.yaml`;
  тест сверяет с ним весь зарегистрированный набор маршрутов.

Сквозной тест `TestRealSessionCatalogAndProfileFlow` работает с PostgreSQL:
регистрация admin, вступление viewer, каталог, профиль, запрет записи viewer,
CSRF, неверный config и отзыв сессии. `TestProfileCatalogSecretBoundary`
проверяет сохранение/очистку/скрытие секретов и безопасные ошибки.

Auth и profiles сохраняют аудит/изменения в своих транзакционных таблицах
`auth_audit/auth_changes` и `profile_audit/profile_changes`. Общий WSS publisher
пока не подключён к этим журналам. Агентский TCP runtime, команды, телеметрия
и доставка catalog.changed также остаются отдельной работой; исправление
совместимости не означает готовность всех модулей 02–11.
