# Генерация CA и отдельных TLS-сертификатов для API, PostgreSQL, Redis и Kafka.
# openssl берём из Docker-образа (ставить на хост не нужно).
# Запускать из папки honeyforge:  .\gen-certs.ps1
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path "./certs" | Out-Null
docker run --rm --entrypoint /bin/sh -v "${PWD}/certs:/certs" `
  -v "${PWD}/deploy/tls/generate.sh:/generate.sh:ro" alpine/openssl /generate.sh
if ($LASTEXITCODE -ne 0) { throw "TLS certificate generation failed" }
Write-Host "Сертификаты созданы в ./certs. Доверяйте ca.crt; CA сохраняется между запусками."
