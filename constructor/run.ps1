$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    docker compose up --build -d
    if ($LASTEXITCODE -ne 0) { throw "HoneyForge startup failed" }
    Write-Host "HoneyForge Constructor: https://localhost:8443/app/constructor"
} finally {
    Pop-Location
}
