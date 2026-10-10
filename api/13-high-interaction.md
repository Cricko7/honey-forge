# SSH high interaction

Тип каталога: `ssh-high-interaction/1`, уровень `high`. Реальная Linux-среда
с OpenSSH, bash и coreutils. Внешний SSH-шлюз записывает трафик и управляет
отдельным контейнером на SSH-соединение. Shell выполняется пользователем
`sandbox` (UID 1000), независимо от имени-приманки при входе. Полного root,
systemd и отдельного ядра в этом варианте нет.

## Запуск демо

Требуются Docker Engine с Linux-контейнерами и основной стек из
[REAL-STACK.md](../REAL-STACK.md). Compose собирает SSH-шлюз и образ среды.

```sh
docker compose -f docker-compose.real.yml up --build -d
```

В интерфейсе создайте профиль типа **SSH High Interaction**, затем ловушку.
Шаблон профиля:

```json
{
  "services": [{"name": "ssh", "port": 2222, "username": "root", "password": "DemoTrapOnly!"}],
  "session": {
    "max_seconds": 300,
    "max_connections": 4,
    "max_recording_bytes": 1048576,
    "max_upload_bytes": 1048576
  },
  "management": {"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 500}
}
```

Пароль-приманка скрывается при чтении профиля. Образы задаются только через
доверенную конфигурацию развертывания `DECOY_SSH_IMAGE` и `SSH_SANDBOX_IMAGE`;
профиль не позволяет выбрать произвольный образ или параметры Docker.

После успешных команд `apply_config` и `start`:

```sh
ssh -p 2222 root@localhost
sftp -P 2222 root@localhost
```

Это два разных соединения, поэтому у них разные временные файловые системы.
Чтобы загрузить файл и открыть shell в одной среде, используйте SSH multiplexing
на Linux/macOS (ControlMaster может отсутствовать в Windows OpenSSH):

```sh
ssh -M -S /tmp/hf-demo.sock -p 2222 root@localhost
# В другом терминале:
sftp -o ControlPath=/tmp/hf-demo.sock -P 2222 root@localhost
# В SFTP: put ./demo.txt /home/sandbox/demo.txt
# В первом shell: cat /home/sandbox/demo.txt; rm /home/sandbox/demo.txt
```

Удаление файла в среде не удаляет сохранённые блоки. При завершении соединения
или по абсолютному таймауту контейнер удаляется. Ключ внешнего SSH-шлюза
генерируется при запуске worker; после его перезапуска клиенту потребуется
обновить запись known_hosts для демо-порта.

## Записи и файлы

Откройте любое событие этой ловушки в разделе **События**, нажмите **Запись
SSH-сессии**. Доступны текстовый просмотр потоков, выгрузка полного JSON и
скачивание SFTP-файлов. Вывод показывается как текст, без исполнения управляющих
последовательностей. Это журнал блоков с метками времени, а не эмулятор терминала.
Файлы скачиваются как `<file_id>.bin` после проверки SHA-256; исходный путь
показывается отдельно и никогда не используется как путь на компьютере оператора.

REST: `GET /api/events/recordings/{session_id}?trap_id={trap_id}`. Доступ:
admin/viewer той же организации. Чужая ловушка/сессия возвращает 404, без сессии
оператора — 401. Нет новых таблиц: данные сохраняются существующим журналом
агента и PostgreSQL-телеметрией. `complete` означает непрерывную последовательность
от открытия до закрытия соединения; успешность каждого файла определяется
отдельным `service.file_closed.complete`. При открытой сессии нажмите **Обновить**.

События:

| Тип | Данные |
| --- | --- |
| `service.connection_opened` | `service` |
| `service.session_io` | `channel_id`, `stream` (`stdin/stdout/stderr/exec`), `payload_base64`, `bytes` |
| `service.file_opened` | `file_id`, исходный `path` |
| `service.file_chunk` | `file_id`, `offset`, `payload_base64`, `bytes` |
| `service.file_closed` | `file_id`, `size`, `sha256`, `complete` |
| `service.connection_closed` | `duration_ms`, `bytes_received`, `reason` |

Каждый блок содержит до 4096 бинарных байт. Сначала сохраняется событие,
затем данные передаются в среду. Запись `exec` сохраняет строку команды; ввод
и вывод интерактивного shell идут отдельными потоками. Изменение размера PTY
передаётся в OpenSSH, но не входит в текстовую запись. При аварии агента журнал
закрывает сессию с сохранением последнего номера события.

`max_recording_bytes`: суммарные stdin/stdout/stderr/exec байты на соединение.
`max_upload_bytes`: суммарные записанные SFTP-байты и суммарный размер создаваемых
артефактов на соединение. Пределы из каталога — до 16 MiB каждого вида записи,
до 16 соединений, 10–3600 секунд. Дополнительно: до 8 SSH-каналов на соединение,
16 одновременно открытых файлов на SFTP-канал, 8192 событий плюс
финальное закрытие. Превышение суммарного объёма останавливает соединение; причина
видна в `connection_closed`. Недопустимый размер или offset отдельной записи
отклоняется без передачи данных в среду. Сбой сохранения останавливает шлюз, чтобы не продолжать
незаписанную сессию.

Поддерживаются свежие SFTP-загрузки с truncate, чтение, list/stat, mkdir,
rename, remove и chmod. Append/resume, SFTP-ссылки и произвольный setstat
отклоняются. SCP через legacy `scp -O` не сохраняет отдельные файловые артефакты;
используйте SFTP. Файлы, созданные командами shell, не выделяются в отдельные
артефакты, но терминальный трафик записывается.

## Граница изоляции

Среда: `--network none`, read-only корень, tmpfs для home/tmp, UID 1000,
все capabilities удалены, `no-new-privileges`, 128 MiB RAM, 0.5 CPU, 64 PID.
Нет host mounts, опубликованных портов, Docker socket, токенов агента или
сетевого пути к API/Redis/Интернету. OpenSSH внутри контейнера использует только
stdin/stdout транспорта `docker exec`, с одноразовым ключом шлюза. Forwarding
и пользовательские переменные окружения через SSH отключены.

Доверенный SSH-шлюз, как оркестратор, имеет Docker socket и работает на manager
узле. Его worker не понижает UID: атакующий shell всегда в другом контейнере.
Защита зависит от Docker, ядра хоста и SSH-шлюза; контейнер не заменяет VM.
Оркестратор удаляет среды по host-side TTL, в том числе после падения шлюза.
В текущем варианте SSH-шлюз и среды предназначены для одного manager-узла;
образ среды должен быть собран на том же Docker Engine.

## Проверка с настоящим контейнером

Этот тест запускает Linux-среду, проверяет shell, UID, отсутствие сети и
секретов, загружает бинарный файл по SFTP, читает его через shell того же
соединения и проверяет удаление контейнера. Запуск с корня репозитория
в Linux-окружении с Docker Engine:

```sh
docker build -f deploy/decoys/Dockerfile.sandbox -t honeyforge/ssh-sandbox:test .
cd src/backend
TEST_SSH_DOCKER=1 SSH_SANDBOX_IMAGE=honeyforge/ssh-sandbox:test \
  go test -race -tags=integration ./internal/decoys/sshtrap \
  -run TestDockerRealLinuxShellSFTPAndIsolation -count=1 -timeout=120s -v
```

Для проверки хранения записи и доступа между организациями задайте
`TEST_DATABASE_URL` для отдельной тестовой PostgreSQL-базы и выполните
`go test -tags=integration ./internal/app -run TestSSHRecordingPersistsAndRespectsOrganization -count=1 -v`.
Без соответствующих переменных эти тесты пропускаются; обычные unit-тесты
не подтверждают работоспособность Docker и PostgreSQL.
