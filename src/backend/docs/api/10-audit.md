# 10. Журнал аудита оператора

## Передача модуля

- Нужно: 01–09; записи подключить к действиям 02/04/05/06.
- Отдаёт: AuditEntry и audit.created, неизменяемая история.
- Приёмка: Один audit на действие, без второго на replay и без секретов/атакующего input.
- Общий порядок и границы: [README](README.md).

## Схемы

| Схема | Поля |
|---|---|
| AuditEntry | `id: ID`, `occurred_at: Timestamp`, `actor: AuditActor`, `action: string` (1..100), `resource: AuditResource`, `details: AuditDetails` |
| AuditActor | `user_id: ID`, `email: string`, `role: "admin" | "viewer"` |
| AuditResource | `kind: "organization" | "profile" | "trap" | "command" | "session"`, `id: ID` |
| AuditDetails | `changed_fields?: string[]` (JSON Pointer), `profile_revision?: Revision`, `trap_id?: ID`, `command_id?: ID`, `credential_generation?: Revision` |

Аудит содержит только разрешённые выше metadata, а не произвольную копию тела. Password, cookie, CSRF token, join_code, agent token, config секреты и payload атак отсутствуют. Организация определяется сервером. AuditActor фиксирует роль/email на момент действия; будущие изменения не переписывают историю.

Обязательные action: organization.created, organization.viewer_joined, organization.join_code_rotated, session.created, session.revoked, profile.created, profile.updated, profile.deleted, trap.created, trap.updated, trap.deleted, trap.agent_credentials_issued, trap.agent_credentials_revoked, command.created. Успешный idempotent replay не создаёт вторую запись. Heartbeat, доставка атаки и каждый просмотр списка не являются действиями оператора и не засоряют этот журнал. Результат выполнения команды хранится в Command и command.changed, а не записывается как действие пользователя. Stop/start — command.created, не дополнительные trap.updated. Захваченные логины/пароли и действия атакующего относятся к Event.data, а не к аудиту оператора. Удаление Trap сохраняет AuditResource.id для истории.

Регистрация создаёт audit в новой/существующей организации после появления пользователя. session resource.id — безопасный audit ID сессии, не cookie и не идентификатор для входа. Ошибка операции не записывается как успешное action. Изменение и его audit должны становиться видимыми согласованно; технический отказ не оставляет успешное действие без требуемого аудита.

## GET /api/audit-entries

Admin/viewer. limit/cursor, `from?`, `to?`, `action?` (string 1..100), `actor_id?` (ID), `resource_id?` (ID). `200 Page<AuditEntry>`, occurred_at DESC/id DESC. Фильтры AND; валидный неизвестный action или actor_id — пустая Page. Никакой информации другой организации. GET не создаёт запись аудита сам на себя.

## GET /api/audit-entries/{id}

Admin/viewer. `200 AuditEntry`; чужой/отсутствующий — 404 resource_not_found. Нет POST/PATCH/DELETE оператора: аудит неизменяем. Уведомление audit.created содержит только audit_id, затем frontend может прочитать ресурс.
