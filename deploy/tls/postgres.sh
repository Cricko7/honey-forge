#!/bin/sh
set -eu
mkdir -p /tmp/honeyforge-tls
cp /certs/postgres.crt /tmp/honeyforge-tls/server.crt
cp /certs/postgres.key /tmp/honeyforge-tls/server.key
chown -R postgres:postgres /tmp/honeyforge-tls
chmod 600 /tmp/honeyforge-tls/server.key
exec /usr/local/bin/docker-entrypoint.sh postgres \
  -c ssl=on -c ssl_min_protocol_version=TLSv1.2 \
  -c ssl_cert_file=/tmp/honeyforge-tls/server.crt \
  -c ssl_key_file=/tmp/honeyforge-tls/server.key "$@"
