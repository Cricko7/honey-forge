param([switch]$Check)

$ErrorActionPreference = 'Stop'
Push-Location (Join-Path (Split-Path -Parent $PSScriptRoot) 'src/backend')
try {
    $formatMode = if ($Check) { '-l' } else { '-w' }
    $formatOutput = & go run golang.org/x/tools/cmd/goimports@v0.51.0 -local honey-forge $formatMode ./cmd ./internal ./modules
    if ($LASTEXITCODE -ne 0) {
        throw 'goimports failed.'
    }
    if ($Check -and $formatOutput) {
        $formatOutput | Write-Output
        throw 'Run ./scripts/format.ps1 to format these files.'
    }
} finally {
    Pop-Location
}
