#!/usr/bin/env bash
# Добавляет ловушку в HoneyForge через REST. Панель (оркестратор) сама поднимает
# контейнер приманки и запускает listener — ручной агент не нужен.
#
# Примеры:
#   # первый запуск на чистой базе (регистрирует организацию):
#   ./add-trap.sh --register --org "OOO Perimetr" \
#       --email admin@perimetr.ru --password demo-password-2026 \
#       --name SSH-DMZ --type tcp-banner --port 2222 --wait
#
#   # ещё одна ловушка (вход существующим админом):
#   ./add-trap.sh --email admin@perimetr.ru --password demo-password-2026 \
#       --name Redis-prod --type redis-emulator --port 6380 --wait
set -euo pipefail

BASE_URL="https://localhost:8443"
EMAIL=""; PASSWORD=""; REGISTER=0; ORG="HoneyForge Org"
NAME=""; TYPE="tcp-banner"; PORT=2222
BANNER='SSH-2.0-OpenSSH_8.9p1\r\n'; REDIS_PASSWORD="S3cretTrap!"
CONFIG_FILE=""; WAIT=0

while [ $# -gt 0 ]; do
  case "$1" in
    --base-url) BASE_URL="$2"; shift 2;;
    --email) EMAIL="$2"; shift 2;;
    --password) PASSWORD="$2"; shift 2;;
    --register) REGISTER=1; shift;;
    --org) ORG="$2"; shift 2;;
    --name) NAME="$2"; shift 2;;
    --type) TYPE="$2"; shift 2;;
    --port) PORT="$2"; shift 2;;
    --banner) BANNER="$2"; shift 2;;
    --redis-password) REDIS_PASSWORD="$2"; shift 2;;
    --config-file) CONFIG_FILE="$2"; shift 2;;
    --wait) WAIT=1; shift;;
    *) echo "Неизвестный аргумент: $1" >&2; exit 2;;
  esac
done
[ -n "$EMAIL" ] && [ -n "$PASSWORD" ] && [ -n "$NAME" ] || { echo "Нужны --email, --password, --name" >&2; exit 2; }

JAR="$(mktemp)"; trap 'rm -f "$JAR"' EXIT
ORIGIN="$BASE_URL"
UUID() { cat /proc/sys/kernel/random/uuid 2>/dev/null || python3 -c 'import uuid;print(uuid.uuid4())'; }
jget()  { curl -sk -b "$JAR" "$BASE_URL$1"; }
jpost() { # path json [csrf]
  local h=(-H "Origin: $ORIGIN" -H "Content-Type: application/json")
  [ -n "${3:-}" ] && h+=(-H "X-CSRF-Token: $3")
  curl -sk -b "$JAR" -c "$JAR" -X POST "$BASE_URL$1" "${h[@]}" --data "$2"
}
jqv() { sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p" <<<"$2" | head -n1; }

echo "[1/3] Аутентификация ($BASE_URL)..."
if [ "$REGISTER" = "1" ]; then
  RESP="$(jpost /api/registrations "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"organization\":{\"mode\":\"create\",\"name\":\"$ORG\"}}")"
else
  RESP="$(jpost /api/sessions "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")"
fi
CSRF="$(jqv csrf_token "$RESP")"
[ -n "$CSRF" ] || { echo "Ошибка входа: $RESP" >&2; exit 1; }
echo "      ok"

echo "[2/3] Создание профиля ($TYPE, порт $PORT)..."
if [ -n "$CONFIG_FILE" ]; then
  CONFIG="$(cat "$CONFIG_FILE")"
elif [ "$TYPE" = "redis-emulator" ]; then
  CONFIG="{\"services\":[{\"name\":\"redis\",\"port\":$PORT,\"password\":\"$REDIS_PASSWORD\"}],\"management\":{\"heartbeat_interval_seconds\":10,\"telemetry_flush_interval_ms\":500}}"
else
  CONFIG="{\"listeners\":[{\"name\":\"svc\",\"port\":$PORT,\"banner\":\"$BANNER\",\"close_after_banner\":false}],\"logging\":{\"capture_payload\":true,\"max_payload_bytes\":4096},\"management\":{\"heartbeat_interval_seconds\":10,\"telemetry_flush_interval_ms\":500}}"
fi
PROF="$(jpost /api/profiles "{\"request_id\":\"$(UUID)\",\"name\":\"$NAME-profile\",\"type_id\":\"$TYPE\",\"type_version\":1,\"config\":$CONFIG}" "$CSRF")"
PID="$(jqv id "$PROF")"
[ -n "$PID" ] || { echo "Профиль не создан: $PROF" >&2; exit 1; }
echo "      profile $PID"

echo "[3/3] Создание ловушки '$NAME' (панель поднимет её сама)..."
TRAP="$(jpost /api/traps "{\"request_id\":\"$(UUID)\",\"name\":\"$NAME\",\"profile_id\":\"$PID\"}" "$CSRF")"
TID="$(jqv id "$TRAP")"
[ -n "$TID" ] || { echo "Ловушка не создана: $TRAP" >&2; exit 1; }
echo "      trap $TID"

if [ "$WAIT" = "1" ]; then
  echo "Ожидание авто-деплоя оркестратором..."
  for _ in $(seq 1 18); do
    sleep 5
    T="$(jget "/api/traps/$TID")"
    RS="$(jqv runtime_state "$T")"
    echo "      runtime=$RS"
    [ "$RS" = "running" ] && break
  done
  [ "${RS:-}" = "running" ] && echo "ГОТОВО: ловушка работает на порту $PORT." \
    || echo "Пока не running. Смотрите: docker compose -f docker-compose.real.yml logs orchestrator"
fi
echo "trap_id=$TID"
