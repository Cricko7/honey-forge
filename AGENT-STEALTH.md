# Маскировка агента на хосте (anti-fingerprint / anti-tamper)

Дата: 2026-10-11.

## Зачем
Цель — **чтобы атакующий, получивший обзор хоста (`ps`, `docker ps/inspect`,
`/proc/<pid>/environ`, `strings <binary>`, `docker network/secret ls`), не опознал
агента как honeypot и не снёс его**. Агент маскируется под обычный служебный демон
телеметрии узла — **легенда `node-metrics`**. Явный «honeypot/decoy/C2» камуфляж убран,
имя/путь/лейблы/env переведены на нейтральный неймспейс `node-metrics` / `NM_*` /
`com.nodemetrics.*`. Это дополняет уже имевшуюся живучесть (`--restart-condition
on-failure`, изоляция дочернего процесса, конфиг только через stdin — не через argv).

## Что видно атакующему и как замаскировано

| Поверхность | Было (маркер honeypot) | Стало (легенда node-metrics) |
|---|---|---|
| Имя процесса / бинарь | `edge-worker` в `/app/edge-worker` | `node-metrics` в `/usr/local/bin/node-metrics` |
| Swarm-сервис | `hf-trap-<id>` | `node-metrics-<id>` |
| SSH-песочница (контейнер) | `hf-session-<id>` | `node-metrics-session-<id>` |
| Префикс секретов | `hf-<id>-…` | `nm-<id>-…` |
| Overlay-сеть (локальный стек) | `hf-decoys` | `nm-mesh` |
| Redis-ключ журнала | `honey-forge:{<id>}:journal` | `nm:{<id>}:journal` |

## Полная карта строк, изменённых в бинаре агента (`strings node-metrics`)

Бинарь собирается из `./cmd/agent`. Ниже — каждая строка-литерал, которая раньше
выдавала honeypot, и во что переименована. Producer (оркестратор) и consumer (агент)
меняются синхронно, чтобы деплой не сломался.

### Имена env-переменных (видны в `/proc/<pid>/environ` и `docker inspect`)

| Было | Стало | Файлы |
|---|---|---|
| `AGENT_WS_URL` | `NM_WS_URL` | `cmd/agent/main.go`, `internal/agent/run.go`, `orchestrator/docker_deploy.go` |
| `AGENT_TRAP_ID` | `NM_INSTANCE_ID` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go` |
| `AGENT_TOKEN` | `NM_TOKEN` | `cmd/agent/main.go` |
| `AGENT_TOKEN_FILE` | `NM_TOKEN_FILE` | `cmd/agent/main.go`, `internal/agent/process_unix.go`, `orchestrator/docker_deploy.go` |
| `AGENT_REDIS_URL` | `NM_STORE_URL` | `cmd/agent/main.go`, `internal/agent/journal_redis.go` |
| `AGENT_REDIS_URL_FILE` | `NM_STORE_URL_FILE` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go` |
| `AGENT_CA_FILE` | `NM_CA_FILE` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go` |
| `AGENT_REDIS_CA_FILE` | `NM_STORE_CA_FILE` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go` |
| `DECOY_TYPE` | `NM_ROLE` | `cmd/agent/main.go`, `internal/agent/child.go`, `internal/agent/process_unix.go`, `deploy/decoys/Dockerfile` |
| `SSH_SANDBOX_IMAGE` (на ловушке) | `NM_SANDBOX_IMAGE` | `internal/decoys/sshtrap/runtime.go`, `orchestrator/docker_deploy.go` |
| `AGENT_CERT_FILE` (mTLS, добавлен коллегами в e8b7277) | `NM_CERT_FILE` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go` |
| `AGENT_KEY_FILE` (mTLS) | `NM_KEY_FILE` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go` |
| `AGENT_SERVER_PINS_FILE` (mTLS) | `NM_SERVER_PINS_FILE` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go` |

Секреты mTLS на ловушке: `agent_cert`/`agent_key`/`server_pins` → `nm_cert`/`nm_key`/`nm_server_pins`.
Новые лейблы из e8b7277: `honeyforge.certificate-expires` → `com.nodemetrics.certificate-expires`,
`honeyforge.tls-trust` → `com.nodemetrics.tls-trust` (`orchestrator/docker.go`, `docker_deploy.go`).

### Изоляция воркера (MASK-3, e8f476b) — воркер запускается отдельным Docker-контейнером
| Было | Стало | Файл |
|---|---|---|
| `TRAP_WORKER_IMAGE` | `NM_WORKER_IMAGE` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go`, compose |
| `TRAP_WORKER_NETWORK` | `NM_WORKER_NETWORK` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go`, compose |
| `TRAP_STDIO` | `NM_STDIO` | `cmd/agent/main.go`, `internal/agent/process_docker.go`, `deploy/decoys/entrypoint.sh` |
| `DECOY_BIND_IP` (на ловушке) | `NM_BIND_IP` | `cmd/agent/main.go`, `orchestrator/docker_deploy.go`, compose |
| контейнер воркера `hf-worker-<id>` | `nm-worker-<id>` | `internal/agent/process_docker.go` |
| лейбл `honeyforge.worker` | `com.nodemetrics.worker` | `process_docker.go`, `orchestrator/docker.go` |
| лейбл `honeyforge.isolation` | `com.nodemetrics.isolation` (значение `mask3` оставлено как непрозрачный sentinel) | `orchestrator/docker.go`, `docker_deploy.go` |
| бинарь воркер-образа `/agent` | `/usr/local/bin/node-metrics` | `deploy/decoys/Dockerfile`, `deploy/decoys/entrypoint.sh` |

Примечание: management-plane оркестратора читает собственный конфиг `DECOY_BIND_IP`/`DECOY_NETWORK`/`AGENT_WS_URL`/`MGMT_NETWORK`
(`cmd/orchestrator/main.go`) и проставляет на ловушку уже `NM_*` — имена независимы, рассинхрона нет.

Фильтр окружения, передаваемого дочернему `--worker` процессу, теперь вырезает
префикс `NM_` (`internal/agent/process.go`) вместо `AGENT_`/`TRAP_`.

### Строки-ошибки (тоже попадают в бинарь)

| Было | Стало | Файл |
|---|---|---|
| `AGENT_WS_URL must be wss://host/assets/stream` | `NM_WS_URL must be wss://host/assets/stream` | `internal/agent/run.go` |
| `AGENT_TOKEN, AGENT_TRAP_ID and AGENT_REDIS_URL are required` | `NM_TOKEN, NM_INSTANCE_ID and NM_STORE_URL are required` | `internal/agent/run.go` |
| `AGENT_REDIS_URL is required` | `NM_STORE_URL is required` | `cmd/agent/main.go` |
| `AGENT_REDIS_URL must use rediss:// with certificate verification` | `NM_STORE_URL must use rediss:// …` | `internal/agent/journal_redis.go` |
| `SSH_SANDBOX_IMAGE is required` | `NM_SANDBOX_IMAGE is required` | `internal/decoys/sshtrap/runtime.go` |
| `snapshot type does not match decoy image` | `snapshot type does not match image` | `internal/agent/child.go` |
| `configure agent TLS` / `configure Redis TLS` | `configure node TLS` / `configure store TLS` | `cmd/agent/main.go` |
| лог `agent stopped` | `node-metrics stopped` | `cmd/agent/main.go` |

### Docker-лейблы (видны в `docker inspect` / `docker service inspect`)

| Было | Стало |
|---|---|
| `honeyforge.trap` | `com.nodemetrics.instance` |
| `honeyforge.generation` | `com.nodemetrics.generation` |
| `honeyforge.ports` | `com.nodemetrics.ports` |
| `honeyforge.sandbox` | `com.nodemetrics.sandbox` |
| `honeyforge.expires` | `com.nodemetrics.expires` |

Читатели/писатели лейблов: `orchestrator/docker.go`, `orchestrator/docker_deploy.go`,
`orchestrator/sandbox_cleanup.go`, `decoys/sshtrap/docker.go`.

### Имена секретов / сервиса / контейнера (код)

| Было | Стало | Файл |
|---|---|---|
| `serviceName = "hf-trap-" + id` | `"node-metrics-" + id` | `orchestrator/docker.go` |
| `base = "hf-%s-%d"` | `"nm-%s-%d"` | `orchestrator/docker_deploy.go` |
| `HasPrefix(name, "hf-")` | `HasPrefix(name, "nm-")` | `orchestrator/docker.go` |
| target секретов `agent_token` / `redis_url` / `agent_ca` / `redis_ca` | `nm_token` / `store_url` / `nm_ca` / `store_ca` | `orchestrator/docker_deploy.go` |
| sandbox `"hf-session-" + id` + префикс `/hf-session-` | `"node-metrics-session-" + id` + `/node-metrics-session-` | `decoys/sshtrap/docker.go`, `orchestrator/sandbox_cleanup.go` |

### Сборка бинаря (меньше символов в strings)

`Dockerfile.agent` и `deploy/decoys/Dockerfile`:
- выходной бинарь `edge-worker`/`agent` → `node-metrics`, путь `/usr/local/bin/node-metrics`;
- добавлен `-ldflags="-s -w"` (убраны таблица символов и DWARF);
- `-trimpath` оставлен (убирает абсолютные пути файлов).
- `deploy/decoys/Dockerfile`: `ENV DECOY_TYPE=…` → `ENV NM_ROLE=…` (значение вшито в
  attacker-видимый образ ловушки).

## Остаточные маркеры (НЕ убраны в этой итерации)

1. **Путь Go-модуля `honey-forge/...`** всё ещё присутствует в `gopclntab` бинаря
   (имена функций для паник). `-ldflags="-s -w"` это не убирает. Полное устранение:
   либо переименовать go-модуль `honey-forge` → нейтральный во ВСЁМ бэкенде (go.mod +
   все импорты), либо собирать через `garble -literals -tiny build`. Отложено сознательно
   (высокий риск/объём, нужен `go build` для проверки).
2. **Repo-имена образов `honeyforge/*-decoy`, `honeyforge/ssh-sandbox`** видны через
   `docker inspect --format '{{.Config.Image}}'`. Они вплетены в `.github/workflows/ci-cd.yml`
   (build-теги, `release.env`) и прод-деплой. Чтобы убрать консистентно — переименовать
   теги во всех трёх местах (CI build, `release.env`, значения `DECOY_*_IMAGE` в compose),
   напр. `nodemetrics/tcp:…`. Локальный стек это не ломает (имена — дефолты с override).
3. **Сеть `hf-decoys`** переименована в `nm-mesh` только в локальном `docker-compose.real.yml`.
   В `.github/workflows/ci-cd.yml` (healthcheck) и прод-окружении всё ещё `hf-decoys` —
   чтобы совпало, выставить `DECOY_NETWORK=nm-mesh` и поправить две строки `docker inspect`
   в CI (стр. ~280-281).
4. **Типы событий контракта** (`service.auth_attempt`, `honeytoken.triggered` и т.п.)
   НЕ трогались — это часть OpenAPI-контракта между агентом и бэком; переименование
   сломало бы совместимость. Атакующему в payload они не видны.

## Тесты — ОБНОВЛЕНЫ ✅

Переименование строковое: `go build ./...` не ломает, но тесты со старыми именами
ломали бы `go test`. Все затронутые тесты приведены к новым именам и проверены в Docker
(`go build` + `go vet ./...` + `go vet -tags=integration ./...` = зелёно).

**Обновлены под новые имена (agent/orchestrator-side):**
- `cmd/agent/main_test.go`, `internal/agent/{run,stdio,process_docker,process_docker_integration}_test.go` → `NM_*`
- `internal/orchestrator/{docker,docker_states,sandbox_cleanup,ssh,honeytokens}_test.go` → лейблы `com.nodemetrics.*`, `node-metrics-…`, префикс `nm-`
- `internal/decoys/sshtrap/{docker,docker_integration}_test.go` → `node-metrics-session-…`, `NM_SANDBOX_IMAGE`

**Намеренно НЕ тронуты (management-plane — не видны атакующему):**
- `internal/app/endpoint_deployment_test.go`, `endpoint_proxy_integration_test.go`
- `internal/platform/tlsconfig/deployment_test.go` — проверяет `AGENT_REDIS_URL` у сервиса
  `api` (inline rediss://); агент всегда использовал `*_FILE`, менять нечего, проверка валидна.

## Замечание про management-plane
Env оркестратора/API (`cmd/orchestrator/main.go`, `cmd/api/main.go`, `internal/app/endpoint.go`)
намеренно оставлены как `AGENT_*`/`DECOY_*`/`SSH_SANDBOX_IMAGE` — это контейнеры панели
управления, НЕ видны атакующему, и их имена независимы от того, что оркестратор проставляет
на attacker-видимой ловушке (`NM_*`). Это осознанное ограничение объёма, а не рассинхрон.
