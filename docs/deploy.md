# Deploying Talaria

Talaria is one static binary. Production is a folder plus a service:

```
C:\Talaria\
  talaria.exe        the binary (talaria.exe.prev is kept for rollback)
  talaria.toml       config (also editable from Settings → Admin in the app)
  data\              SQLite database + uploads
  talaria.log        service log
  deploy.ps1         install-and-restart script that CI calls
```

(A Linux equivalent lives in `deploy/` too: `install.sh`, `deploy.sh`, and a
systemd unit, with the same shape.)

## One-time Windows server setup

1. Grab a Windows build (`talaria-windows-amd64.exe`) from the latest CI run
   or GitHub Release, and the `deploy/` folder from the repo.
2. In an **elevated** PowerShell:
   ```powershell
   cd deploy
   .\install.ps1 -Binary ..\talaria-windows-amd64.exe
   ```
   This creates `C:\Talaria`, opens the firewall for the binary, and registers
   the **Talaria** Windows service (auto-start at boot, auto-restart on crash).
   The service is built into the binary itself: `talaria -service install|uninstall|start|stop`.
3. Copy `talaria.toml` and the `data\` folder from the old machine into
   `C:\Talaria`, then `Start-Service Talaria`.
4. Point the router's port forwards (TCP 80/443, UDP 50000) at the new box.
   DuckDNS and the home-network domain update themselves on first start.

> **Smart App Control.** Windows 11 machines with Smart App Control set to
> *On* refuse to run unsigned binaries, which is what CI produces. The
> installer warns if it is on. Turn it off (Windows Security → App & browser
> control → Smart App Control) or sign the builds before relying on CD.

## Continuous deployment

`.github/workflows/ci.yml` builds the web client, runs the Go tests,
cross-compiles for Linux/Windows/macOS, and on every push to `main` runs a
`deploy` job on a **self-hosted GitHub Actions runner** on the server. The
runner polls GitHub, so nothing inbound is exposed. To add it:

1. Repo → Settings → Actions → Runners → **New self-hosted runner** → Windows.
2. Download into `C:\actions-runner`, then configure it as a service running as
   SYSTEM (so it may restart Talaria), with the label `talaria-server`:
   ```powershell
   .\config.cmd --url https://github.com/OWNER/talaria --token <TOKEN> `
     --labels talaria-server --unattended --runasservice --windowslogonaccount "NT AUTHORITY\SYSTEM"
   ```

The deploy job downloads the fresh Windows binary and calls
`C:\Talaria\deploy.ps1`, which smoke-tests it, swaps it in, restarts the
service, waits for `/healthz`, and rolls back automatically if the new build
does not come up. Manual rollback: `C:\Talaria\deploy.ps1 -Rollback`.

## Releases

Pushing a tag like `v0.3.0` publishes a GitHub Release with binaries for every
platform and a `SHA256SUMS.txt`.

## Logs

```powershell
Get-Content C:\Talaria\talaria.log -Tail 50 -Wait
```
