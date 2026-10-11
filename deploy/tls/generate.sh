#!/bin/sh
set -eu
umask 077
cd "${CERT_DIR:-/certs}"

# Keep the installation CA stable so existing agent trust and Docker secrets
# remain valid. Rotation of this CA is an explicit deployment operation.
if [ ! -f ca.crt ] && [ ! -f ca.key ]; then
  openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 3650 \
    -keyout ca.key -out ca.crt -subj '/CN=HoneyForge local CA' \
    -addext 'basicConstraints=critical,CA:TRUE' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign'
fi
test -s ca.crt && test -s ca.key
openssl x509 -in ca.crt -checkend 86400 -noout
if [ "$(openssl x509 -in ca.crt -pubkey -noout)" != "$(openssl pkey -in ca.key -pubout)" ]; then
  echo 'CA key does not match ca.crt' >&2
  exit 1
fi

issue() {
  name=$1
  sans=$2
  if { [ "$name" != resources ] || [ "${ROTATE_SERVER_KEY:-0}" != 1 ]; } && \
     [ -s "$name.crt" ] && [ -s "$name.key" ] && \
     openssl verify -CAfile ca.crt "$name.crt" >/dev/null 2>&1 && \
     openssl x509 -in "$name.crt" -checkend 86400 -noout >/dev/null && \
     [ "$(openssl x509 -in "$name.crt" -pubkey -noout)" = "$(openssl pkey -in "$name.key" -pubout)" ]; then
    return
  fi
  openssl req -new -newkey rsa:2048 -nodes -sha256 \
    -keyout "$name.key" -out "$name.csr" -subj "/CN=$name"
  printf '%s\n' 'basicConstraints=critical,CA:FALSE' \
    'keyUsage=critical,digitalSignature,keyEncipherment' \
    'extendedKeyUsage=serverAuth' "subjectAltName=$sans" > "$name.ext"
  openssl x509 -req -in "$name.csr" -CA ca.crt -CAkey ca.key \
    -CAcreateserial -out "$name.crt" -days 365 -sha256 -extfile "$name.ext"
  rm -f "$name.csr" "$name.ext"
}

pin() {
  printf 'sha256/'
  openssl x509 -in resources.crt -pubkey -noout | \
    openssl pkey -pubin -outform DER | openssl dgst -sha256 -binary | openssl base64 -A
  printf '\n'
}
# Preserve the previous pin while agents receive the new immutable trust secret.
if [ -s resources.crt ] && [ ! -s server-pins ]; then pin > server-pins; fi
issue localhost 'DNS:localhost,DNS:api,IP:127.0.0.1'
issue resources 'DNS:resources.localhost,DNS:localhost,IP:127.0.0.1'
issue postgres 'DNS:postgres,DNS:localhost,IP:127.0.0.1'
issue redis 'DNS:redis,DNS:localhost,IP:127.0.0.1'
issue kafka 'DNS:kafka,DNS:localhost,IP:127.0.0.1'

# A dedicated client CA prevents infrastructure server keys from authenticating agents.
if [ ! -f agents-ca.crt ] && [ ! -f agents-ca.key ]; then
  openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 3650 \
    -keyout agents-ca.key -out agents-ca.crt -subj '/CN=Resource client CA' \
    -addext 'basicConstraints=critical,CA:TRUE' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign'
fi
test -s agents-ca.crt && test -s agents-ca.key
openssl x509 -in agents-ca.crt -checkend 86400 -noout
if [ "$(openssl x509 -in agents-ca.crt -pubkey -noout)" != "$(openssl pkey -in agents-ca.key -pubout)" ]; then
  echo 'Client CA key does not match agents-ca.crt' >&2
  exit 1
fi

client() {
  name=$1
  identity=$2
  if [ "${ROTATE_CLIENT_KEYS:-0}" != 1 ] && [ -s "$name.crt" ] && [ -s "$name.key" ] && \
     openssl verify -purpose sslclient -CAfile agents-ca.crt "$name.crt" >/dev/null 2>&1 && \
     openssl x509 -in "$name.crt" -checkend 86400 -noout >/dev/null && \
     [ "$(openssl x509 -in "$name.crt" -subject -noout -nameopt RFC2253)" = "subject=CN=$identity" ] && \
     [ "$(openssl x509 -in "$name.crt" -pubkey -noout)" = "$(openssl pkey -in "$name.key" -pubout)" ]; then
    return
  fi
  openssl req -new -newkey rsa:2048 -nodes -sha256 \
    -keyout "$name.key" -out "$name.csr" -subj "/CN=$identity"
  printf '%s\n' 'basicConstraints=critical,CA:FALSE' \
    'keyUsage=critical,digitalSignature' 'extendedKeyUsage=clientAuth' > "$name.ext"
  openssl x509 -req -in "$name.csr" -CA agents-ca.crt -CAkey agents-ca.key \
    -CAcreateserial -out "$name.crt" -days 30 -sha256 -extfile "$name.ext"
  rm -f "$name.csr" "$name.ext"
}
client resource-proxy resource-proxy
if [ -n "${AGENT_TRAP_ID:-}" ]; then
  case "$AGENT_TRAP_ID" in
    *[!a-fA-F0-9-]*|'') echo 'Invalid AGENT_TRAP_ID' >&2; exit 1 ;;
  esac
  test "${#AGENT_TRAP_ID}" -eq 36
  client agent "$AGENT_TRAP_ID"
fi
new_pin=$(pin)
if [ ! -s server-pins ] || ! grep -qxF "$new_pin" server-pins; then
  printf '%s\n' "$new_pin" >> server-pins
fi
chmod 644 server-pins
cat kafka.key kafka.crt > kafka.pem
# Apache Kafka runs as UID 1000. Other services stage keys from root-only mounts.
chown 1000:1000 kafka.pem
chmod 600 ./*.key kafka.pem
chmod 644 ./*.crt
cat > kafka-client.properties <<'EOF'
security.protocol=SSL
ssl.truststore.type=PEM
ssl.truststore.location=/certs/ca.crt
ssl.endpoint.identification.algorithm=https
EOF
chmod 644 kafka-client.properties
echo 'TLS certificates ready. Trust certs/ca.crt; keep ca.key private.'
