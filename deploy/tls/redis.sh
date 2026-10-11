#!/bin/sh
set -eu
mkdir -p /tmp/honeyforge-tls
cp /certs/redis.crt /tmp/honeyforge-tls/server.crt
cp /certs/redis.key /tmp/honeyforge-tls/server.key
chown -R redis:redis /tmp/honeyforge-tls
chmod 600 /tmp/honeyforge-tls/server.key
exec /usr/local/bin/docker-entrypoint.sh redis-server \
  --port 0 --tls-port 6379 --tls-protocols 'TLSv1.2 TLSv1.3' \
  --tls-cert-file /tmp/honeyforge-tls/server.crt \
  --tls-key-file /tmp/honeyforge-tls/server.key \
  --tls-ca-cert-file /certs/ca.crt --tls-auth-clients no \
  --appendonly yes --appendfsync always --maxmemory-policy noeviction "$@"
