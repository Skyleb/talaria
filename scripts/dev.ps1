# Dev workflow: Go API on :8080 (plain HTTP, no browser auto-open) + Vite on :5173.
# Browse http://localhost:5173 — Vite proxies /api, /files, /avatars to Go.
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent

$devConfig = Join-Path $root 'dev.lantern.toml'
if (-not (Test-Path $devConfig)) {
  @'
[server]
name = "Lantern Dev"
http_port = 8080
data_dir = "./data-dev"
max_upload_mb = 500
open_browser = false

[tls]
mode = "off"
'@ | Out-File -Encoding utf8 $devConfig
}

$go = Start-Process -PassThru -NoNewWindow go -ArgumentList 'run', './cmd/lantern', '--config', $devConfig -WorkingDirectory $root
try {
  Set-Location (Join-Path $root 'web')
  npm run dev
} finally {
  if (-not $go.HasExited) { Stop-Process -Id $go.Id -Force }
}
