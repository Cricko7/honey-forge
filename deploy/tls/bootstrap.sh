#!/bin/sh
set -eu
umask 077
cd /certs
exec 9>/certs/bootstrap.lock
flock 9

# Persist random local secrets; restarts must not change database credentials.
if [ ! -s postgres-password ]; then
  if [ -n "${POSTGRES_PASSWORD:-}" ]; then printf '%s' "$POSTGRES_PASSWORD" > postgres-password;
  else openssl rand -hex 24 > postgres-password; fi
elif [ -n "${POSTGRES_PASSWORD:-}" ] && [ "$(cat postgres-password)" != "$POSTGRES_PASSWORD" ]; then
  echo 'POSTGRES_PASSWORD differs from the saved database password' >&2
  exit 1
fi
if [ ! -s cursor-key ]; then
  if [ -n "${CURSOR_KEY:-}" ]; then printf '%s' "$CURSOR_KEY" > cursor-key;
  else openssl rand -base64 32 > cursor-key; fi
fi

/bin/sh /tls/generate.sh

# Each consumer gets only its own material; signing keys stay out of decoys/web.
publish() {
  destination=$1
  shift
  mkdir -p "/exports/$destination"
  for file in "$@"; do
    cp "$file" "/exports/$destination/$file"
    case "$file" in
      *.crt|kafka-client.properties|server-pins) chmod 644 "/exports/$destination/$file" ;;
      *) chmod 600 "/exports/$destination/$file" ;;
    esac
  done
}
publish postgres postgres.crt postgres.key postgres-password
publish redis redis.crt redis.key ca.crt
publish kafka kafka.pem ca.crt kafka-client.properties
chown 1000:1000 /exports/kafka/kafka.pem
publish api localhost.crt localhost.key ca.crt agents-ca.crt postgres-password cursor-key
publish web localhost.crt localhost.key resources.crt resources.key ca.crt agents-ca.crt resource-proxy.crt resource-proxy.key
publish orchestrator ca.crt agents-ca.crt agents-ca.key server-pins postgres-password
publish agent ca.crt server-pins
if [ -n "${AGENT_TRAP_ID:-}" ]; then publish agent agent.crt agent.key; fi
touch /certs/bootstrap-ready
