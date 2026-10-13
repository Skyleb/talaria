# Deploying Talaria

Talaria is one static binary. Production is a folder plus a service:

```
/opt/talaria/
  talaria            the binary (talaria.prev is kept for rollback)
  talaria.toml       config (also editable from Settings → Admin in the app)
  deploy.sh          install-and-restart script that CI calls
<data_dir>/          SQLite database + uploads; default /opt/talaria/data,
                     or a big disk such as /srv/talaria/data
```

Logs go to the journal: `journalctl -u talaria -f`.

(Windows equivalents live in `deploy/` too: `install.ps1`, `deploy.ps1`, and
a built-in service mode, `talaria -service install|start|stop|uninstall`.)

## One-time Linux server setup (Ubuntu/Debian)

1. Grab a Linux build (`talaria-linux-amd64`) from the latest CI run or
   GitHub Release, and the `deploy/` folder from the repo.
2. Install, optionally pointing the data dir at a big disk:
   ```bash
   cd deploy
   sudo ./install.sh "$USER" /srv/talaria/data
   ```
   This creates the `talaria` system user and `/opt/talaria`, writes the
   systemd unit (auto-start, auto-restart, sandboxed to those two folders), a
   starter `talaria.toml`, and a sudoers rule that lets the given user run
   `/opt/talaria/deploy.sh` as root and nothing else.
3. Copy `talaria.toml` and the old `data/` folder over (database + uploads),
   `sudo chown -R talaria:talaria` both, then do the first deploy:
   ```bash
   sudo /opt/talaria/deploy.sh ./talaria-linux-amd64
   ```
4. Point the router's port forwards (TCP 80/443, UDP 50000) at the new box.
   DuckDNS and the home-network domain update themselves on first start.

## Continuous deployment

`.github/workflows/ci.yml` builds the web client, runs the Go tests,
cross-compiles for Linux/Windows/macOS, and on every push to `main` runs a
`deploy` job on a **self-hosted GitHub Actions runner** on the server. The
runner polls GitHub, so nothing inbound is exposed. To add it:

1. Repo → Settings → Actions → Runners → **New self-hosted runner** → Linux.
2. Download into `~/actions-runner` as the user you gave `install.sh`, then:
   ```bash
   ./config.sh --url https://github.com/Skyleb/talaria --token <TOKEN> \
     --labels talaria-server --unattended
   sudo ./svc.sh install && sudo ./svc.sh start
   ```

The deploy job downloads the fresh Linux binary and calls
`/opt/talaria/deploy.sh`, which smoke-tests it, swaps it in, restarts the
service, waits for `/healthz`, and rolls back automatically if the new build
does not come up. Manual rollback: `sudo /opt/talaria/deploy.sh --rollback`.

## Releases

Pushing a tag like `v0.3.0` publishes a GitHub Release with binaries for every
platform and a `SHA256SUMS.txt`.
