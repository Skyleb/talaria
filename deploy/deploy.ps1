# Installs a new Talaria binary and restarts the Windows service, keeping the
# previous binary for rollback. Called by CI (the Actions runner service runs
# as SYSTEM) and usable by hand from an elevated PowerShell:
#
#   C:\Talaria\deploy.ps1 -Binary .\talaria-windows-amd64.exe
#   C:\Talaria\deploy.ps1 -Rollback
param(
  [string]$Binary,
  [switch]$Rollback
)
$ErrorActionPreference = 'Stop'
$Dir = 'C:\Talaria'
$Bin = Join-Path $Dir 'talaria.exe'

function Restart-Talaria {
  Stop-Service Talaria -ErrorAction SilentlyContinue
  Start-Service Talaria
}

function Get-Port {
  $toml = Join-Path $Dir 'talaria.toml'
  if (Test-Path $toml) {
    $m = Select-String -Path $toml -Pattern '^\s*http_port\s*=\s*(\d+)' | Select-Object -First 1
    if ($m) { return [int]$m.Matches[0].Groups[1].Value }
  }
  return 8080
}

function Test-Healthy([int]$port) {
  for ($i = 0; $i -lt 25; $i++) {
    try {
      $r = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$port/healthz" -TimeoutSec 2
      if ($r.StatusCode -eq 200) { return $true }
    } catch { }
    Start-Sleep -Seconds 1
  }
  return $false
}

if ($Rollback) {
  if (-not (Test-Path "$Bin.prev")) { throw 'no previous binary to roll back to' }
  Stop-Service Talaria -ErrorAction SilentlyContinue
  Move-Item -Force $Bin "$Bin.bad"
  Move-Item -Force "$Bin.prev" $Bin
  Start-Service Talaria
  Write-Output 'rolled back'
  exit 0
}

if (-not $Binary) { throw 'usage: deploy.ps1 -Binary <new-binary> | -Rollback' }
Copy-Item -Force $Binary "$Bin.new"

# Smoke-test the new binary before touching the service. A failure here on a
# fresh build usually means Smart App Control is blocking unsigned binaries.
$ver = & "$Bin.new" -version 2>&1
if ($LASTEXITCODE -ne 0) { throw "new binary failed to run: $ver" }

Stop-Service Talaria -ErrorAction SilentlyContinue
if (Test-Path $Bin) { Move-Item -Force $Bin "$Bin.prev" }
Move-Item -Force "$Bin.new" $Bin
Start-Service Talaria

$port = Get-Port
if (Test-Healthy $port) {
  Write-Output "deployed $ver, healthy on :$port"
  exit 0
}

Write-Error 'health check failed; rolling back'
Get-Content (Join-Path $Dir 'talaria.log') -Tail 30 -ErrorAction SilentlyContinue | Write-Output
if (Test-Path "$Bin.prev") {
  Stop-Service Talaria -ErrorAction SilentlyContinue
  Move-Item -Force $Bin "$Bin.bad"
  Move-Item -Force "$Bin.prev" $Bin
  Start-Service Talaria
}
exit 1
