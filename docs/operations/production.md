# Production: сервер, конфигурация и перенос

## Состояние установки на 16 октября 2026

| Параметр | Значение |
|---|---|
| Новый сервер платформы | `103.74.94.55`, `msk-1-vm-ghg9` |
| Старый сервер | `147.45.141.37`, `msk-1-vm-mn6c`; остановлен, сохранён для отката |
| ОС нового сервера | Ubuntu 22.04.5 LTS, amd64, 8 ГБ RAM |
| Docker / Compose | 29.9.1 / v5.6.0 |
| Nginx | 1.30.6 из официального репозитория nginx.org |
| Перенесённый релиз | `066d2fd35f38762003fc742034e5dec83060bf40-38864446284-1` |
| Каталог релиза | `/opt/honeyforge/incoming/38864446284-1` |
| Swarm | отдельный кластер, один manager; advertise address `192.168.0.5` |

Это снимок миграции, а не обещание, что данный релиз всегда последний.
Фактические образы смотрите через `docker ps --format 'table {{.Names}}\t{{.Image}}'`.

Подтверждены: запуск PostgreSQL, Kafka и Redis с перенесёнными данными,
`/healthz` API, healthy конструктора, HTTP 200 сайта и шесть сервисов агентов
`1/1`. С локальной машины TCP-порт 35518 отдаёт SSH-баннер, HTTP-порт 55796
отвечает 404 на `/backup.txt`. Этот 404 подтверждает HTTP-ответ, но не
существование honeyfile: используйте путь из профиля. Доставка новых событий,
вход после изменения hosts и первый CI-деплой на новом сервере требуют
отдельного подтверждения. `1/1` не доказывает подключение агента к API.

## Учётные записи экспертов и жюри

Организация: **Madrigal-squad.ru**. Вход: https://honey-forge-seg-fault.ru
(настройка hosts описана ниже).

Таблица содержит пароли доступа: при коммите они попадут в историю Git
и будут доступны читателям репозитория. При изменении паролей обновите таблицу.

| Логин (Email) | Пароль | Роль |
|---|---|---|
| `admin@Madrigal-squad.ru` | `admin@Madrigal-squad.ru` | Администратор / создатель организации |
| `syharik3316@Madrigal-squad.ru` | `syharik3316@Madrigal-squad.ru` | Пользователь организации, разрешено управление ловушками |
| `kdjirl@Madrigal-squad.ru` | `kdjirl@Madrigal-squad.ru` | Пользователь организации, разрешён только просмотр мониторинга |
| `user1@Madrigal-squad.ru` | `user1@Madrigal-squad.ru` | Пользователь организации, разрешён только просмотр мониторинга |
| `user2@Madrigal-squad.ru` | `user2@Madrigal-squad.ru` | Пользователь организации, разрешён только просмотр мониторинга |
| `user3@Madrigal-squad.ru` | `user3@Madrigal-squad.ru` | Пользователь организации, разрешён только просмотр мониторинга |
| `user4@Madrigal-squad.ru` | `user4@Madrigal-squad.ru` | Пользователь организации, разрешён только просмотр мониторинга |
| `user5@Madrigal-squad.ru` | `user5@Madrigal-squad.ru` | Пользователь организации, разрешено управление ловушками |

## Где хранятся настройки

- `/etc/honey-forge/production.env`: пароли БД/Redis, постоянный CURSOR_KEY,
  origins, сетевые и SMTP-настройки. Файл закрытый, не включается в Git.
- `/etc/honey-forge/certs/`, `/etc/honey-forge/postgres-certs/`: сертификаты,
  ключи, CA и pins. Сохраняйте при переносе; не генерируйте новые без плана
  ротации доверия агентов.
- `/etc/nginx/`: конфигурация и сертификаты внешнего HTTPS.
- `/opt/honeyforge/compose.production.yml`: основной серверный Compose.
  Он не хранится в этом репозитории; не подменяйте его локальным Compose.
- `<release>/release.env`: точные ссылки на образы данного релиза.
- `<release>/compose.{ssh,constructor,support,http,native}.yml`: обязательные
  дополнения текущего production-деплоя. SSH-файл генерируется workflow;
  остальные исходники находятся в `deploy/`.
- `/opt/honeyforge/current`: ссылка на последний успешно установленный релиз.
- `/var/www/honeyforge-frontend`: ссылка на `frontend` выбранного релиза.

[production.env.example](../../deploy/production.env.example) — справочник,
не готовая замена действующему файлу. Сохраняйте существующие секреты.
Переменная в env-файле попадёт в контейнер только при наличии соответствующего
`environment` в Compose; особенно проверьте `AGENT_WS_HOST_IP`, SSH и native
image references. Полный `docker compose config` раскрывает секреты:
для проверки используйте `config --quiet`, для образов — `config --images`.

## Команда управления сервером

Установите [hf-compose](../../deploy/hf-compose) из checkout:

```bash
install -m 750 deploy/hf-compose /usr/local/sbin/hf-compose
hf-compose config --quiet
hf-compose ps
hf-compose logs --tail 80 api orchestrator constructor
```

Helper читает `current`, production.env и все пять overrides. Для проверки
конкретного релиза без переключения `current`:

```bash
HF_RELEASE_DIR=/opt/honeyforge/incoming/38864446284-1 hf-compose config --quiet
```

`HF_ROOT` и `HF_CONFIG` позволяют явно выбрать другие каталоги.
`docker compose down -v` и очистку volumes на production не используйте.

## Имена, TLS и сети

Имена `honey-forge-seg-fault.ru` и `resources.honey-forge-seg-fault.ru` в этой
установке синтетические: публичного DNS для них нет. На компьютере оператора
замените прежние записи на строку из [hosts.example](../../deploy/hosts.example).
В Windows путь — `C:\Windows\System32\drivers\etc\hosts`, нужны права
администратора; после изменения выполните `ipconfig /flushdns`.
В WSL отдельно проверьте разрешение имени. CA необходимо доверять независимо
от hosts; не обходите проверку TLS через `curl -k`.

| Сеть | Подсеть | Назначение |
|---|---|---|
| `hf-mgmt` | `10.0.2.0/24` | attachable overlay, управление |
| `hf-decoys` | `10.0.1.0/24` | attachable overlay, приманки |
| `docker_gwbridge` | `172.19.0.0/16`, шлюз `172.19.0.1` | доступ контейнеров к хосту |

Управляемые агенты получают сопоставление ресурсного имени через
`AGENT_WS_HOST_IP=172.19.0.1`. Хостовый `/etc/hosts` контейнеры не наследуют;
`127.0.0.1` внутри контейнера не является адресом платформы.
На другом узле шлюз должен вести к доступному resource endpoint: локальный
адрес manager нельзя слепо переносить на workers.

Nginx должен поддерживать `ssl_reject_handshake` (начиная с 1.19.4).
Штатный Nginx 1.18 Ubuntu 22.04 для этого конфига не подходит. При установке
официального пакета сохраняйте проверку подписей репозитория и конфигурацию;
используйте существующего системного пользователя (`www-data` на новом
сервере), затем `nginx -t` перед reload. Не удаляйте TLS-проверки upstream,
agent mTLS или SNI-защиту ради успешного запуска.

[Шаблон Nginx](../../deploy/nginx/honey-forge.conf) использует loopback и
закрытый корневой маршрут. Это безопасная основа, а не точная копия
действующего сайта. Для панели настройте согласованный management-интерфейс,
SPA `root /var/www/honeyforge-frontend; try_files $uri /index.html;` и include
`/etc/honey-forge/nginx/constructor.location.conf` в management server.
Ресурсный host должен оставаться отдельным от панели. Не расширяйте bind
управления до публичного интерфейса без решения о доступе операторов.

## Данные и процедура миграции

Перенесены четыре named volumes:

| Volume | Содержимое |
|---|---|
| `honeyforge_postgres_data` | пользователи, организации, профили, ловушки, события, аудит |
| `honeyforge_kafka_data` | журнал Kafka и смещения |
| `honeyforge_redis_data` | журнал доставки агентов, AOF |
| `honeyforge_constructor_data` | данные конструктора |

Anonymous volumes Kafka не заменяют основное хранилище. Перед повторной
миграцией заново проверьте все mounts и данные текущей версии.

1. Приостановите новые CI-деплои. Зафиксируйте реальные image references,
   все Compose-файлы и env-file paths по labels работающих контейнеров.
2. Подготовьте Docker, Nginx, отдельный Swarm и сети на новом сервере.
   Не копируйте `/var/lib/docker` целиком и состояние Swarm между хостами.
3. Скопируйте конфигурацию по SSH, сверив host fingerprint; сохраните права
   ключей. Скопируйте образы релиза и точные инфраструктурные образы через
   `docker save/load`. Проверьте `config --quiet` и `nginx -t` до остановки.
4. Остановите старые orchestrator, constructor и Nginx. Сохраните число реплик
   сервисов с label `com.nodemetrics.instance`, уменьшите их до нуля,
   остановите оставшиеся `nm-worker-*`, затем API. Это прекращает запись;
   неподтверждённая телеметрия остаётся в переносимом журнале Redis.
5. Сделайте `pg_dump -Fc`, остановите Kafka, Redis и PostgreSQL с достаточным
   timeout. Убедитесь, что процессы завершились штатно. Копируйте volumes
   только после остановки, сохраняя numeric UID/GID, ACL и extended attributes
   (`rsync -aHAX --numeric-ids`). На целевом сервере данные ещё не используются.
6. Запустите PostgreSQL/Kafka/Redis с `--pull never --wait`, проверьте логи и
   БД. Затем API и `/healthz`, constructor, Nginx, последним orchestrator.
7. Переключите hosts операторов, проверьте вход, сохранённую историю,
   heartbeat, команды и новое событие от внешнего запроса к каждой ловушке.
8. Подготовьте ссылки frontend/current и переключите CI-доступ, как ниже.

Не запускайте одновременно старую и новую копии с одинаковыми идентификаторами
агентов. Старый сервер остановлен, его volumes не удалены. Резервная копия БД
на старом: `/root/honeyforge-backup-20261016074028/honey_forge.dump`.
После первых записей на новом откат на старую БД потеряет новые данные:
сначала остановите новую запись и согласуйте обратный перенос.
Для защиты от случайного запуска старой копии ограничьте доступ и планируйте
отдельное отключение autostart: перезагрузка может активировать systemd/Swarm.

На новом сервере включён `vm.overcommit_memory=1` через
`/etc/sysctl.d/90-honeyforge-redis.conf` для надёжного Redis persistence.

## Завершение переключения CI/CD

В GitHub Actions настройте secrets `SSH_HOST=103.74.94.55`, `SSH_USER` и
`SSH_PRIVATE_KEY`; variables `SSH_PORT=22`, `API_BASE_URL` и
`TELEMETRY_ORIGIN`. Origins сохраняются прежними, меняется адрес SSH.
Публичный ключ CI должен быть в authorized_keys выбранного пользователя;
миграционный ключ не заменяет его. Workflow использует Docker и `sudo -n`:
проверьте доступ для пользователя деплоя. Не публикуйте приватные ключи.

Workflow проверяет, что frontend является symlink. При миграции содержимое
frontend скопировано обычным каталогом; перед первым CI-деплоем исправьте это.
На новом сервере, убедившись в выбранном релизе и наличии frontend/index.html:

```bash
(
set -euo pipefail
release=/opt/honeyforge/incoming/38864446284-1
front=/var/www/honeyforge-frontend
root=/opt/honeyforge
test -s "$release/frontend/index.html"
test -d "$front"
test ! -L "$front"
test ! -e "$root/current.migration"
test ! -L "$root/current.migration"
backup="$front.before-symlink.$(date +%Y%m%d%H%M%S)"
mv "$front" "$backup"
if ! ln -s "$release/frontend" "$front"; then
  mv "$backup" "$front"
  exit 1
fi
ln -s "$release" "$root/current.migration"
mv -Tf "$root/current.migration" "$root/current"
nginx -t
)
```

Сохраните frontend backup. Проверьте чтение статики пользователем Nginx,
вход и `/constructor/` до нового деплоя. Не запускайте повторно этот блок,
если frontend уже ссылка; сначала проверьте её цель. Ссылка `current` должна
быть ссылкой либо отсутствовать, а не реальным каталогом.

После обновления secrets запустите новый workflow для `main`, а не повтор
старого запуска, привязанного к старому commit. Успешный деплой должен
подтвердить API, constructor, frontend и работу агентов. Только после этого
планируйте удаление временных SSH-ключей и вывод старого сервера из эксплуатации.
