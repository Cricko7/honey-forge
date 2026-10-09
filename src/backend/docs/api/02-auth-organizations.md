# 02. Операторы, организации и cookie-сессии

## Передача модуля

- Нужно: 01.
- Отдаёт: AuthContext, cookie/CSRF, роли и организация для защищённых модулей.
- Приёмка: Создание admin, join viewer, login/logout; отказ чужому доступу.
- Общий порядок и границы: [README](README.md).

## Схемы

| Схема | Поля |
|---|---|
| RegisterRequest | `email: string`, `password: string`, `organization: CreateOrganizationInput | JoinOrganizationInput` |
| CreateOrganizationInput | `mode: "create"`, `name: Name` |
| JoinOrganizationInput | `mode: "join"`, `join_code: string` — 32 символа base64url `[A-Za-z0-9_-]` |
| LoginRequest | `email: string`, `password: string` |
| User | `id: ID`, `organization_id: ID`, `email: string`, `role: "admin" | "viewer"`, `created_at: Timestamp` |
| OrganizationSummary | `id: ID`, `name: Name`, `created_at: Timestamp` |
| SessionView | `user: User`, `organization: OrganizationSummary`, `expires_at: Timestamp`, `csrf_token: string` |

Внутренний AuthContext для других модулей: `user_id: ID`, `organization_id: ID`, `role: "admin" | "viewer"`. Он создаётся сервером после проверки активной сессии и не принимается из HTTP-тела. Cookie/пароль/CSRF token не передаются в feature как публичный пользовательский DTO.

Email: валидный email без display name, до 254 ASCII-символов; trim и lowercase перед сравнением и возвратом. Email глобально уникален. Пароль: 12..128 символов и максимум 512 байт UTF-8; без trim/нормализации. Не принимаются поля role/user_id/organization_id. Название организации не уникально; по названию вступать нельзя. Не существует состояния «пользователь без организации».

## POST /api/registrations

Без сессии. Создание организации:

```json
{"email":"admin@example.com","password":"demo-password-2026","organization":{"mode":"create","name":"Demo SOC"}}
```

Вступление:

```json
{"email":"viewer@example.com","password":"demo-password-2026","organization":{"mode":"join","join_code":"0123456789abcdefghijklmnopqrstuv"}}
```

Успех `201`, `Location: /api/session`, тело SessionView и cookie-сессия. Создание организации, admin, кода вступления и сессии атомарно; вступление создаёт viewer и сессию атомарно. Клиент не может выбрать admin при вступлении. Уже аутентифицированному пользователю — `409 already_authenticated` (`Already authenticated`).

`409 email_in_use` (`Email is already registered`); `422 invalid_join_code` (`Invalid organization join code`) одинаково для неизвестного и уже заменённого кода; `422 validation_failed`. Старый код не принимается после завершения ротации. Одновременное создание одинакового email: одна регистрация 201, другая 409; второй пользователь/организация не остаются.

Повтор регистрации не идемпотентен: потеряв ответ, клиент пробует login; если email занят, новая организация не создаётся. Проверка email отдельным публичным endpoint не предоставляется.

## POST /api/sessions

Без сессии; LoginRequest. Успех `201`, `Location: /api/session`, SessionView и новая cookie. Существующая сессия текущего браузера при успешном login заменяется; при неверных credentials остаётся прежней. Неизвестный email и неверный пароль — одинаковый `401 invalid_credentials`. Вход не меняет организацию/роль. Несколько браузерных сессий одного пользователя разрешены.

## GET /api/session

Admin/viewer. `200 SessionView`; отсутствующая/истёкшая/отозванная сессия — `401 unauthenticated`. Frontend получает актуальную роль и CSRF token этим запросом после перезагрузки страницы.

## DELETE /api/session

Для активной сессии требуется CSRF. Отзыв текущей сессии, `204`, очистка cookie. Без активной сессии также `204`; Origin проверяется всегда. Остальные браузерные сессии сохраняются. WSS этой сессии закрывается с 4401.

## Cookie и CSRF

`Set-Cookie: __Host-session=<opaque>; Path=/; Secure; HttpOnly; SameSite=Strict; Max-Age=604800`. Без Domain; срок 7 суток без продления активностью. Пароль и значение сессии никогда не возвращаются в JSON. Ответ login/registration содержит expires_at. Logout выдаёт ту же cookie с `Max-Age=0`.

REST и frontend находятся на одном origin. Все изменяющие запросы требуют точный разрешённый Origin; отсутствие Origin либо mismatch — `403 origin_not_allowed`. Для аутентифицированных POST/PATCH/DELETE дополнительно `X-CSRF-Token`, полученный из SessionView; отсутствие/mismatch — `403 csrf_failed`. Registration/login защищаются проверкой Origin и обязательным application/json, CSRF token до сессии не нужен. GET не требует CSRF.

Для registration/login: максимум 10 попыток за 60 секунд на клиентский IP, независимо от успеха, `429 rate_limited`, Retry-After. Адрес IP определяется сервером, произвольный X-Forwarded-For не является удостоверением клиента. Cookie не отправляется агенту и не принимается для агентского WSS.

Email verification, password reset, удаление аккаунта и смена ролей не являются частью этого happy path и не имеют маршрутов в этой версии.

## Организация и общий код вступления

### Схемы

| Схема | Поля |
|---|---|
| Organization | `id: ID`, `name: Name`, `created_at: Timestamp` |
| JoinCode | `code: string` (32 символа base64url), `revision: Revision`, `rotated_at: Timestamp` |
| RotateJoinCodeRequest | `expected_revision: Revision` |

Один пользователь состоит в одной организации. Все зарегистрированные по коду — viewer. Общий код многоразовый, срок действия не ограничен до замены; код не является идентификатором организации и не даёт доступ к API без регистрации/сессии.

### GET /api/organization

Admin/viewer. `200 Organization` своей организации. Код вступления отсутствует. Изменение названия и переключение организации не включены в текущий контракт.

### GET /api/organization/join-code

Только admin. `200 JoinCode`. Viewer получает `403 forbidden`. Код не содержится в списках пользователей, SessionView, аудитах, логах ответов и WSS сообщениях. Администратор может повторно прочитать действующий код после потери ответа на ротацию.

### POST /api/organization/join-code/rotations

Только admin, CSRF; RotateJoinCodeRequest. `200 JoinCode` с новым кодом и увеличенной revision. `409 join_code_changed` (`Organization join code has changed`), если expected_revision устарела. Несколько конкурентных ротаций одной revision: только одна успешна. Старый код немедленно становится недействительным; существующие пользователи и сессии не меняются.

Регистрация и ротация линеаризуемы: вступление со старым кодом либо полностью завершено до ротации, либо отвергнуто; пользователь не создаётся частично. Код новой организации создаётся при регистрации admin с revision=1. Ротация записывает audit action `organization.join_code_rotated`, без нового/старого кода.
