# 04. Профили

## Передача модуля

- Нужно: 01–03; проверка привязок подключается из 05.
- Отдаёт: Profile для 05, полный ConfigurationSnapshot заданного revision для 06 (shape в 06).
- Приёмка: CRUD/config validation/If-Match; после подключения 05 — profile_in_use.
- Общий порядок и границы: [README](README.md).

## Схемы

| Схема | Поля |
|---|---|
| CreateProfileRequest | `request_id: ID`, `name: Name`, `description?: Description` (default ""), `type_id: TypeID`, `type_version: TypeVersion`, `config: object` |
| PatchProfileRequest | `name?: Name`, `description?: Description`, `config?: object`, `clear_secret_fields?: string[]` (JSON Pointer, unique, max 100) |
| Profile | `id: ID`, `name: Name`, `description: Description`, `type_id: TypeID`, `type_version: TypeVersion`, `interaction_level: string`, `config: object`, `secret_fields_set: string[]`, `revision: Revision`, `created_at: Timestamp`, `updated_at: Timestamp` |

Config валидируется по точной версии каталога. Только для установленных writeOnly-полей значение не возвращается; остальные ключи возвращаются. Первый tcp-banner не содержит writeOnly-полей: secret_fields_set=[] и clear_secret_fields допустим только []. PATCH config заменяет весь объект несекретных настроек, не выполняет рекурсивный merge. Установленные скрытые секреты сохраняются при их пропуске, затем применяется clear_secret_fields, затем переданные новые значения; один путь одновременно в clear и config запрещён. Финальный объект проходит схему, включая required. null не значит удаление. Пустой PATCH — `422 validation_failed`. Передача неизменённых значений не увеличивает revision, возвращается тот же ETag.

type_id/type_version неизменяемы в существующем профиле; для другого типа/версии создаётся новый профиль. Данное ограничение сохраняет интерпретацию старых config и событий. Создание новой версии типа не мигрирует профили автоматически.

## POST /api/profiles

Admin, CSRF. `201 Profile`, Location `/api/profiles/{id}`, ETag, revision=1. Повтор request_id: `200 Profile` первоначальной созданной revision, а не текущего отредактированного состояния; Location указывает актуальный GET. Воспроизведённый ответ маркируется `Idempotency-Replayed: true`; ETag отсутствует, если это уже не текущая revision.

```json
{
  "request_id":"11111111-1111-4111-8111-111111111111",
  "name":"TCP demo",
  "description":"Low TCP banner",
  "type_id":"tcp-banner",
  "type_version":1,
  "config":{
    "listeners":[{"name":"ssh","port":2222,"banner":"SSH-2.0-OpenSSH_9.6\r\n","close_after_banner":true}],
    "logging":{"capture_payload":false,"max_payload_bytes":0},
    "management":{"heartbeat_interval_seconds":10,"telemetry_flush_interval_ms":500}
  }
}
```

`422 unknown_trap_type` (`Unknown trap type version`), `409 trap_type_unavailable` (`Trap type is unavailable for new profiles`), `422 config_invalid` (`Trap configuration is invalid`). fields указывает пути внутри `/config`. Конкурентная публикация каталога не меняет выбранную immutable-схему.

## GET /api/profiles

Admin/viewer. limit/cursor, `type_id?`. `200 Page<Profile>`, created_at DESC, id DESC. Возвращаемые config редактируемые, секреты скрыты. Нет совпадений — пустая Page.

## GET /api/profiles/{id}

Admin/viewer. `200 Profile`, ETag. Чужой/удалённый — `404 resource_not_found`.

## PATCH /api/profiles/{id}

Admin, CSRF, If-Match. `200 Profile`, новая revision и ETag при изменении. Config validation как при создании; schema/type менять нельзя, неизвестный ключ — 400. Можно изменить существующий профиль deprecated-типа, пока runtime поддерживает эту immutable-версию. Снимки ранее применённых revision сохраняют исходные значения.

PATCH НЕ отправляет команды агентам и НЕ меняет desired/applied у ловушек. Применение — команда apply_config конкретной ловушке. После PATCH UI показывает, что текущая revision профиля отличается от desired/applied revision ловушки.

## DELETE /api/profiles/{id}

Admin, CSRF, If-Match. `204`. Если существует неудалённая ловушка с profile_id — `409 profile_in_use` (`Profile is assigned to a trap`). После удаления ловушек профиль удалять можно; исторические события и снимки не теряют type/version/revision. Чужой/повторно удалённый ID — 404. Одновременная привязка новой ловушки и удаление профиля не могут создать висячую ссылку: одна операция получает конфликт.
