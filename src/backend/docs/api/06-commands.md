# 06. Команды управления

## Передача модуля

- Нужно: 01–05; исполнение/lease подключается в 08.
- Отдаёт: Command, закреплённый ConfigurationSnapshot, очередь, ограничения действий.
- Приёмка: Idempotency/конфликт, revision snapshot, одна команда; затем dispatch/result/ack.
- Общий порядок и границы: [README](README.md).

## Схемы команд

| Схема | Поля |
|---|---|
| CreateCommandRequest | `request_id: ID`, `action: Action`, `params: object` |
| Command | `id: ID`, `trap_id: ID`, `request_id: ID`, `action: Action`, `params: object`, `status: "queued" | "running" | "succeeded" | "failed" | "expired"`, `target_profile_revision: Revision|null`, `created_at: Timestamp`, `expires_at: Timestamp`, `started_at: Timestamp|null`, `finished_at: Timestamp|null`, `result: object|null`, `error: RuntimeError|null` |
| ConfigurationSnapshot | `profile_id: ID`, `profile_revision: Revision`, `type_id: TypeID`, `type_version: TypeVersion`, `config: object` |

params/result проверяются по action descriptor точной версии типа. ConfigurationSnapshot передаётся только агенту; содержит полный config с требуемыми секретами. Operator Command не содержит этот snapshot и не раскрывает writeOnly-поля. succeeded: result соответствует result_schema и error=null; failed: result=null и error!=null; queued/running: result/error/finished_at=null; expired: result=null, error.code=command_expired, finished_at заполнен.

## POST /api/traps/{id}/commands

Admin, CSRF. `201 Command`, Location `/api/traps/{id}/commands/{command_id}`. Создание — ещё НЕ выполнение. Повтор request_id с тем же содержимым — `200` первоначальный Command и `Idempotency-Replayed: true`; актуальный статус GET. Другое содержимое — 409 idempotency_conflict. Повтор проверяется ДО command_in_progress.

Для одной ловушки допускается одна незавершённая команда (queued/running). Вторая с новым request_id — `409 command_in_progress` (`Another command is in progress`). Это исключает гонку stop/start/apply. Команда offline-ловушке сохраняется queued и отправляется при следующем hello. Срок выполнения 24 часа от created_at; после него expired, освобождается active_command_id. Отмена команд отдельно не предоставляется.

| action | params | Семантика |
|---|---|---|
| apply_config | `{ "profile_revision": 1 }` | Revision должна совпасть с текущим Profile. Snapshot закрепляется при создании; desired_profile_revision обновляется. Позднее изменение Profile не меняет команду. Агент применяет snapshot; applied_profile_revision меняется только после подтверждённого успеха. |
| start | `{}` | Требует установленной применённой конфигурации (applied_profile_revision != null). Возобновляет после stop с той же revision. desired_state=running; успех runtime_state=running. Уже running — успешный no-op. |
| stop | `{}` | desired_state=stopped; успех runtime_state=stopped. Уже stopped — успешный no-op. Разрешён до первой конфигурации, applied_profile_revision тогда null. |

apply_config сохраняет текущий running/stopped. Перенастройка выполняется целиком: при невозможности применить новый listener/port старая конфигурация сохраняется, Command failed; если откат также не удался, runtime_state=error с config_rollback_failed. desired_revision остаётся заказанной, applied_revision показывает фактическую: расхождение видно оператору. Новая команда apply_config с новым request_id повторяет попытку. start не означает развёртывание агента или ОС.

`409 profile_changed` (`Profile revision has changed`); `409 configuration_not_applied` (`Trap configuration has not been applied`); `422 unsupported_action` (`Action is not supported by this trap type`); `422 command_params_invalid` (`Command parameters are invalid`). Параметры нельзя добавлять мимо descriptor. Неподдерживаемая агентом версия/действие выявляется при hello и команда завершается failed с runtime code unsupported_type/unsupported_action, а не считается выполненной.

## GET /api/traps/{id}/commands

Admin/viewer; limit/cursor, `status?` из enum Command. `200 Page<Command>`, created_at DESC/id DESC. История доступна после удаления своей ловушки по tombstone; чужая/неизвестная ловушка — 404.

## GET /api/traps/{id}/commands/{command_id}

Admin/viewer. `200 Command`. Историческая команда удалённой своей ловушки доступна; Command другого trap_id или организации — 404. Возможные безопасные runtime error codes: port_unavailable, config_apply_failed, config_rollback_failed, runtime_start_failed, runtime_stop_failed, unsupported_type, unsupported_action, command_expired, buffer_unavailable. error.message не содержит пути/credentials/stack trace. Ошибка выполнения приходит как status=failed в ресурсе, а не как HTTP 500 на успешное чтение.
