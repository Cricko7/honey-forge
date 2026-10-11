#!/bin/sh
set -eu
if [ -n "${REDIS_PASSWORD_FILE:-}" ]; then
  REDIS_PASSWORD=$(cat "$REDIS_PASSWORD_FILE")
fi
password=${REDIS_PASSWORD:?Set REDIS_PASSWORD to at least 32 hexadecimal characters}
case "$password" in *[!a-fA-F0-9]*) echo 'REDIS_PASSWORD must be hexadecimal' >&2; exit 1;; esac
[ "${#password}" -ge 32 ] || { echo 'REDIS_PASSWORD must contain at least 32 hexadecimal characters' >&2; exit 1; }
mkdir -p /tmp/honeyforge-tls
umask 077
printf 'requirepass %s\n' "$password" > /tmp/honeyforge-tls/auth.conf
cp /certs/redis.crt /tmp/honeyforge-tls/server.crt
cp /certs/redis.key /tmp/honeyforge-tls/server.key
chown -R redis:redis /tmp/honeyforge-tls
chmod 600 /tmp/honeyforge-tls/server.key
exec /usr/local/bin/docker-entrypoint.sh redis-server /tmp/honeyforge-tls/auth.conf \
  --protected-mode yes \
  --port 0 --tls-port 6379 --tls-protocols 'TLSv1.2 TLSv1.3' \
  --tls-cert-file /tmp/honeyforge-tls/server.crt \
  --tls-key-file /tmp/honeyforge-tls/server.key \
  --tls-ca-cert-file /certs/ca.crt --tls-auth-clients no \
  --appendonly yes --appendfsync always --maxmemory-policy noeviction "$@"
