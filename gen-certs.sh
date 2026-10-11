#!/bin/sh
set -eu
cd "$(dirname "$0")"
mkdir -p certs
docker run --rm --entrypoint /bin/sh -v "$PWD/certs:/certs" \
  -v "$PWD/deploy/tls/generate.sh:/generate.sh:ro" alpine/openssl /generate.sh
