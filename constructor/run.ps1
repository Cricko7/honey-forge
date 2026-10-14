# Запуск конструктора цепочек HoneyForge (Windows PowerShell)
# Использует venv проекта. Открыть: http://127.0.0.1:8077
$ErrorActionPreference = "Stop"
$root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent   # ...\PythonProject
$py = Join-Path $root ".venv\Scripts\python.exe"
if (-not (Test-Path $py)) { $py = "python" }
Write-Host "HoneyForge Constructor → http://127.0.0.1:8077" -ForegroundColor Magenta
& $py -m uvicorn honeyforge.constructor.backend.app:app --host 127.0.0.1 --port 8077 --app-dir $root
