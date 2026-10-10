<#
.SYNOPSIS
  Добавляет ловушку в HoneyForge через REST. Панель (оркестратор) сама поднимает
  контейнер приманки, применяет конфиг и запускает listener — ручной агент не нужен.

.DESCRIPTION
  Делает: вход (или регистрацию admin с -Register) -> создание профиля под тип
  каталога -> создание ловушки. Дальше при AUTO_DEPLOY_TRAPS=true оркестратор
  разворачивает Swarm-сервис автоматически. С -Wait скрипт дожидается running.

.EXAMPLE
  # первый запуск на чистой базе — заодно регистрирует организацию:
  ./add-trap.ps1 -Register -Org "OOO Perimetr" -Email admin@perimetr.ru -Password demo-password-2026 `
                 -Name "SSH-DMZ" -Type tcp-banner -Port 2222 -Wait

.EXAMPLE
  # добавить ещё одну ловушку (вход существующим админом):
  ./add-trap.ps1 -Email admin@perimetr.ru -Password demo-password-2026 -Name "Redis-prod" -Type redis-emulator -Port 6380 -Wait
#>
[CmdletBinding()]
param(
  [string]$BaseUrl = "https://localhost:8443",
  [Parameter(Mandatory)] [string]$Email,
  [Parameter(Mandatory)] [string]$Password,
  [switch]$Register,
  [string]$Org = "HoneyForge Org",
  [Parameter(Mandatory)] [string]$Name,
  [ValidateSet("tcp-banner","redis-emulator")] [string]$Type = "tcp-banner",
  [int]$Port = 2222,
  [string]$Banner = "SSH-2.0-OpenSSH_8.9p1`r`n",
  [string]$RedisPassword = "S3cretTrap!",
  [string]$ConfigFile,
  [switch]$Wait
)

$ErrorActionPreference = "Stop"
$cookie = Join-Path $env:TEMP ("hf-" + [guid]::NewGuid().ToString("N") + ".cookies")
$origin = $BaseUrl
function Jpost($path, $json, $csrf) {
  $tmp = New-TemporaryFile
  [System.IO.File]::WriteAllText($tmp, $json)
  $hdr = @('-H', "Origin: $origin", '-H', 'Content-Type: application/json')
  if ($csrf) { $hdr += @('-H', "X-CSRF-Token: $csrf") }
  $out = curl.exe -sk -b $cookie -c $cookie -X POST "$BaseUrl$path" @hdr --data "@$tmp"
  Remove-Item $tmp -ErrorAction SilentlyContinue
  return $out
}

Write-Host "[1/3] Аутентификация ($BaseUrl)..." -ForegroundColor Cyan
if ($Register) {
  $body = @{ email=$Email; password=$Password; organization=@{ mode="create"; name=$Org } } | ConvertTo-Json
  $resp = Jpost "/api/registrations" $body $null | ConvertFrom-Json
} else {
  $body = @{ email=$Email; password=$Password } | ConvertTo-Json
  $resp = Jpost "/api/sessions" $body $null | ConvertFrom-Json
}
if (-not $resp.csrf_token) { throw "Не удалось войти/зарегистрироваться: $($resp | ConvertTo-Json -Compress)" }
$csrf = $resp.csrf_token
Write-Host "      ok: $($resp.user.email) / org '$($resp.organization.name)'" -ForegroundColor Green

Write-Host "[2/3] Создание профиля ($Type, порт $Port)..." -ForegroundColor Cyan
if ($ConfigFile) {
  $config = Get-Content -Raw $ConfigFile | ConvertFrom-Json
} elseif ($Type -eq "redis-emulator") {
  $config = @{
    services   = @(@{ name="redis"; port=$Port; password=$RedisPassword })
    management = @{ heartbeat_interval_seconds=10; telemetry_flush_interval_ms=500 }
  }
} else {
  $config = @{
    listeners  = @(@{ name="svc"; port=$Port; banner=$Banner; close_after_banner=$false })
    logging    = @{ capture_payload=$true; max_payload_bytes=4096 }
    management = @{ heartbeat_interval_seconds=10; telemetry_flush_interval_ms=500 }
  }
}
$profReq = @{ request_id=[guid]::NewGuid().ToString(); name="$Name-profile"; type_id=$Type; type_version=1; config=$config } | ConvertTo-Json -Depth 8
$prof = Jpost "/api/profiles" $profReq $csrf | ConvertFrom-Json
if (-not $prof.id) { throw "Профиль не создан: $($prof | ConvertTo-Json -Compress)" }
Write-Host "      profile $($prof.id) rev $($prof.revision)" -ForegroundColor Green

Write-Host "[3/3] Создание ловушки '$Name' (панель поднимет её сама)..." -ForegroundColor Cyan
$trapReq = @{ request_id=[guid]::NewGuid().ToString(); name=$Name; profile_id=$prof.id } | ConvertTo-Json
$trap = Jpost "/api/traps" $trapReq $csrf | ConvertFrom-Json
if (-not $trap.id) { throw "Ловушка не создана: $($trap | ConvertTo-Json -Compress)" }
Write-Host "      trap $($trap.id) [$($trap.runtime_state)/$($trap.connectivity)]" -ForegroundColor Green

if ($Wait) {
  Write-Host "Ожидание авто-деплоя оркестратором..." -ForegroundColor Cyan
  $deadline = (Get-Date).AddSeconds(90)
  do {
    Start-Sleep -Seconds 5
    $t = curl.exe -sk -b $cookie "$BaseUrl/api/traps/$($trap.id)" | ConvertFrom-Json
    Write-Host "      conn=$($t.connectivity) runtime=$($t.runtime_state) applied_rev=$($t.applied_profile_revision)"
  } while ($t.runtime_state -ne "running" -and (Get-Date) -lt $deadline)
  if ($t.runtime_state -eq "running") {
    Write-Host "ГОТОВО: ловушка работает на порту $Port. Подключитесь к ней для генерации событий." -ForegroundColor Green
  } else {
    Write-Host "Пока не running. Проверьте: docker compose -f docker-compose.real.yml logs orchestrator" -ForegroundColor Yellow
  }
}
Remove-Item $cookie -ErrorAction SilentlyContinue
Write-Host "trap_id=$($trap.id)"
