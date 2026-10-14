$ErrorActionPreference = 'Stop'

Push-Location $PSScriptRoot
try {
    docker compose up --build -d
    if ($LASTEXITCODE -ne 0) { throw 'HoneyForge startup failed' }
    Write-Host 'HoneyForge: https://localhost:8443'
    Write-Host 'Constructor: https://localhost:8443/app/constructor'
} finally {
    Pop-Location
}
