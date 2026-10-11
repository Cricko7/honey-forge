# Генерация CA и отдельных TLS-сертификатов для API, PostgreSQL, Redis и Kafka.
# openssl берём из Docker-образа (ставить на хост не нужно).
# Запускать из папки honeyforge:  .\gen-certs.ps1
param(
  [string]$AgentTrapID = $env:AGENT_TRAP_ID,
  [switch]$RotateClientKeys,
  [switch]$RotateServerKey
)
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path "./certs" | Out-Null
$rotate = if ($RotateClientKeys) { "1" } else { "0" }
$rotateServer = if ($RotateServerKey) { "1" } else { "0" }
docker run --rm --entrypoint /bin/sh -e "AGENT_TRAP_ID=$AgentTrapID" -e "ROTATE_CLIENT_KEYS=$rotate" -e "ROTATE_SERVER_KEY=$rotateServer" -v "${PWD}/certs:/certs" `
  -v "${PWD}/deploy/tls/generate.sh:/generate.sh:ro" alpine/openssl /generate.sh
if ($LASTEXITCODE -ne 0) { throw "TLS certificate generation failed" }
Write-Host "Сертификаты созданы в ./certs. Доверяйте ca.crt; CA сохраняется между запусками."
