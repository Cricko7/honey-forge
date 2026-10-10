# Генерация self-signed TLS-сертификата для локального реального стека.
# openssl берём из Docker-образа (ставить на хост не нужно).
# Запускать из папки honeyforge:  .\gen-certs.ps1
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path "./certs" | Out-Null
docker run --rm -v "${PWD}/certs:/certs" alpine/openssl req -x509 -newkey rsa:2048 -nodes `
  -keyout /certs/localhost.key -out /certs/localhost.crt -days 365 `
  -subj "/CN=localhost" `
  -addext "subjectAltName=DNS:localhost,DNS:api,IP:127.0.0.1"
Write-Host "Сертификаты созданы в ./certs (localhost.crt, localhost.key)"
