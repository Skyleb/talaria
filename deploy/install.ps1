# One-time setup of a Windows machine as the Talaria production server.
# Run from an elevated PowerShell in the folder containing a Talaria binary:
#
#   .\install.ps1 -Binary .\talaria-windows-amd64.exe
#
# It creates C:\Talaria, installs the binary + deploy.ps1, opens the firewall
# for it, registers the "Talaria" Windows service (auto-start, auto-restart),
# and prints the GitHub Actions runner steps for continuous deployment.
param(
  [Parameter(Mandatory)][string]$Binary
)
$ErrorActionPreference = 'Stop'
$Dir = 'C:\Talaria'
$Here = Split-Path -Parent $MyInvocation.MyCommand.Path

if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw 'run this from an elevated (Administrator) PowerShell'
}

# Smart App Control blocks unsigned binaries; CI builds are unsigned.
try {
  $sac = (Get-MpComputerStatus).SmartAppControlState
  if ($sac -eq 'On') {
    Write-Warning 'Smart App Control is ON on this machine. It will block the unsigned binaries CI builds.'
    Write-Warning 'Either turn it off (Windows Security > App & browser control > Smart App Control) or sign the builds.'
  }
} catch { }

New-Item -ItemType Directory -Force -Path (Join-Path $Dir 'data') | Out-Null
Copy-Item -Force (Join-Path $Here 'deploy.ps1') (Join-Path $Dir 'deploy.ps1')
Copy-Item -Force $Binary (Join-Path $Dir 'talaria.exe')

$exe = Join-Path $Dir 'talaria.exe'
& $exe -version | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'the binary refuses to run here (Smart App Control?)' }

# Firewall: the service listens on 80/443/8080 (HTTP/S), 50000/udp (calls), 53 (optional LAN DNS).
foreach ($p in @('Private', 'Public')) {
  if (-not (Get-NetFirewallRule -DisplayName "Talaria ($p)" -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -DisplayName "Talaria ($p)" -Program $exe -Direction Inbound -Action Allow -Profile $p | Out-Null
  }
}

if (-not (Test-Path (Join-Path $Dir 'talaria.toml'))) {
  Write-Output 'No talaria.toml yet: copy the one from your old server into C:\Talaria (and the data folder).'
}

if (Get-Service Talaria -ErrorAction SilentlyContinue) {
  Write-Output 'Service already installed.'
} else {
  & $exe -service install -config (Join-Path $Dir 'talaria.toml')
}

Write-Output @"

Talaria host prepared at $Dir.
  1. Copy talaria.toml and the data\ folder from the old machine into $Dir.
  2. Start it:   talaria -service start      (or: Start-Service Talaria)
     Logs:       Get-Content $Dir\talaria.log -Tail 50 -Wait
  3. Continuous deployment: in the GitHub repo go to Settings > Actions > Runners > New self-hosted runner (Windows),
     download the runner into C:\actions-runner and configure it as a service running as SYSTEM so it can restart Talaria:
       .\config.cmd --url https://github.com/OWNER/talaria --token <TOKEN> --labels talaria-server --unattended --runasservice --windowslogonaccount "NT AUTHORITY\SYSTEM"
     Every push to main then runs $Dir\deploy.ps1 with the fresh build (health-checked, auto-rollback).
"@
