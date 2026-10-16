param(
    [switch]$Preflight,
    [string]$Site,
    [string]$HostAddress,
    [int]$PortBase = 34000,
    [switch]$NoBrowser
)
$ErrorActionPreference = 'Stop'
$env:PYTHONUTF8 = '1'
$bundlePython = Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe'
$python = $null
$pythonPrefix = @()
if (Test-Path -LiteralPath $bundlePython) {
    $python = $bundlePython
} elseif (Get-Command py -ErrorAction SilentlyContinue) {
    $python = 'py'
    $pythonPrefix = @('-3')
} elseif (Get-Command python -ErrorAction SilentlyContinue) {
    $python = 'python'
} else {
    throw 'Install Python 3.11 or newer and run start.cmd again.'
}
& $python @pythonPrefix -c 'import sys; assert sys.version_info >= (3, 11), "Python 3.11+ is required"'
if ($LASTEXITCODE -ne 0) { throw 'Python 3.11+ is required.' }
& $python @pythonPrefix -c 'import paramiko, websocket'
if ($LASTEXITCODE -ne 0) {
    & $python @pythonPrefix -m pip install -r (Join-Path $PSScriptRoot 'requirements.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Dependency installation failed.' }
}
$demoArguments = @((Join-Path $PSScriptRoot 'demo.py'), '--port-base', [string]$PortBase)
if ($Preflight) { $demoArguments += '--preflight' }
if ($Site) { $demoArguments += @('--site', $Site) }
if ($HostAddress) { $demoArguments += @('--host', $HostAddress) }
if ($NoBrowser) { $demoArguments += '--no-browser' }
& $python @pythonPrefix @demoArguments
exit $LASTEXITCODE
