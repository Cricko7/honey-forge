#!/bin/sh
set -eu
rm -f /certs/bootstrap-ready
while :; do
  /bin/sh /tls/bootstrap.sh
  sleep 43200
done
