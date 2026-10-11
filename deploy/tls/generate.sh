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
  if [ -s "$name.crt" ] && [ -s "$name.key" ] && \
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

issue localhost 'DNS:localhost,DNS:api,IP:127.0.0.1'
issue postgres 'DNS:postgres,DNS:localhost,IP:127.0.0.1'
issue redis 'DNS:redis,DNS:localhost,IP:127.0.0.1'
issue kafka 'DNS:kafka,DNS:localhost,IP:127.0.0.1'
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
