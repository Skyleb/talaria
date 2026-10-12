# Cross-compile Lantern for every supported platform.
# Run from anywhere: pwsh -File scripts/build.ps1
# Assumes web client is already built (npm run build in web/).
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

if (-not (Test-Path 'internal/web/dist/assets')) {
  Write-Host 'Building web client first...'
  Push-Location web
  npm run build
  if ($LASTEXITCODE -ne 0) { throw 'web build failed' }
  Pop-Location
}

$version = 'dev'
try { $version = (git describe --tags --always --dirty 2>$null) } catch {}
if (-not $version) { $version = 'dev' }

$env:CGO_ENABLED = '0'
$targets = @(
  @{ os = 'windows'; arch = 'amd64'; out = "dist/lantern-windows-amd64.exe" },
  @{ os = 'windows'; arch = 'arm64'; out = "dist/lantern-windows-arm64.exe" },
  @{ os = 'darwin';  arch = 'arm64'; out = "dist/lantern-darwin-arm64" },
  @{ os = 'darwin';  arch = 'amd64'; out = "dist/lantern-darwin-amd64" },
  @{ os = 'linux';   arch = 'amd64'; out = "dist/lantern-linux-amd64" },
  @{ os = 'linux';   arch = 'arm64'; out = "dist/lantern-linux-arm64" }
)

New-Item -ItemType Directory -Force dist | Out-Null
foreach ($t in $targets) {
  $env:GOOS = $t.os
  $env:GOARCH = $t.arch
  Write-Host "Building $($t.out) ..."
  go build -trimpath -ldflags "-s -w -X main.version=$version" -o $t.out ./cmd/lantern
  if ($LASTEXITCODE -ne 0) { throw "build failed for $($t.os)/$($t.arch)" }
}
Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
Write-Host "Done. Binaries in dist/ (version $version)"
Get-ChildItem dist | Where-Object Name -like 'lantern-*' | Select-Object Name, @{N='MB';E={[math]::Round($_.Length/1MB,1)}}
