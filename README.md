# 🪽 Talaria

**Your own private Discord, in one file.** Talaria is a self-hosted chat
server for a group of friends: text channels and DMs, voice and video calls
with screen share, listening to music and videos together, file sharing, and
an installable phone app with notifications. It is a single executable with
no installer, no database to set up and no accounts with anyone else. Put it
on any Windows, macOS or Linux machine that stays on, and your friends connect
from anywhere through their browser or the installed app.

- **Chat** — channels and groups, DMs and group DMs, replies, emoji
  reactions, @mentions, read states, typing, search, a media gallery of
  everything shared, file and image sharing with inline playback, GIF search.
- **Calls** — voice, video and screen share (with system/tab audio) through a
  built-in media server. Per-person volume, automatic loudness levelling,
  speaking indicators, noise suppression, and picture quality that adapts to
  each viewer (or is picked per stream, up to 4K 60 fps).
- **Listen together** — inside any call: a shared queue of YouTube videos and
  uploaded songs that everyone hears at the same moment, with a host who
  drives playback and music that ducks while people talk.
- **Phone app** — installable web app for Android and iPhone with push
  notifications for DMs, mentions and incoming calls.
- **Private** — everything lives in one folder on your machine. HTTPS with
  automatic Let's Encrypt certificates and built-in dynamic DNS for home
  connections.

## Quick start

1. **Download** the file for your system from the
   [latest release](../../releases/latest):

   | System | File |
   |---|---|
   | Windows | `talaria-windows-amd64.exe` |
   | macOS (Apple silicon) | `talaria-darwin-arm64` |
   | macOS (Intel) | `talaria-darwin-amd64` |
   | Linux (x86-64) | `talaria-linux-amd64` |
   | Linux (Raspberry Pi 4/5, ARM servers) | `talaria-linux-arm64` |

2. **Put it in its own folder** (for example `Talaria`). The config file and
   your data will be created next to it.

3. **Run it.**
   - **Windows:** double-click the `.exe`. SmartScreen will say the publisher
     is unknown because the build is not code-signed: click *More info*, then
     *Run anyway*. (If Smart App Control is on, Windows blocks unsigned
     programs entirely; turn it off under Windows Security → App & browser
     control, or build from source.)
   - **macOS:** open Terminal in the folder and run
     `chmod +x talaria-darwin-arm64 && xattr -d com.apple.quarantine talaria-darwin-arm64 && ./talaria-darwin-arm64`
     (the `xattr` step clears the "unidentified developer" block once).
   - **Linux:** `chmod +x talaria-linux-amd64 && ./talaria-linux-amd64`

4. **Set up.** Your browser opens `http://localhost:8080/setup`. Pick a name
   for your server and create your admin account. That is the whole install.

5. **Invite friends.** Everything is in the app: *Settings → Admin* creates
   invite links for the server; the *Invite people* button in a group makes a
   link that also drops them into that group. Anyone who registers through
   your link becomes your friend automatically.

Talaria keeps running while the window (or terminal) is open; close it to
stop. See [Keep it running](#keep-it-running) to make it start with the
machine.

### What gets created

```
Talaria/
  talaria-windows-amd64.exe   the program
  talaria.toml                settings (also editable in the app)
  data/                       database and uploaded files
```

Back up by copying the folder. Move the folder and the server moves with it.
**To update**, download the new release over the old file and start it again:
the database upgrades itself.

## Let friends in from the internet

Out of the box Talaria is reachable on your own network (`http://<your LAN
IP>:8080`, shown when it starts). For friends elsewhere you need a name that
points at your home connection and three forwarded ports. Browsers also only
allow microphones and cameras on HTTPS, so calls need this step too.

1. **Forward ports** on your router to the machine running Talaria:
   **TCP 80**, **TCP 443**, and **UDP 50000** (calls). See
   [`docs/port-forwarding.md`](docs/port-forwarding.md) for router and
   firewall details. If your ISP uses CGNAT (your router's WAN address differs
   from what "what is my IP" sites show) inbound connections cannot reach you;
   ask the ISP for a public IP or use a tunnel such as Tailscale Funnel.

2. **Get a name.** Either:
   - **Free: a Talaria name.** In the setup wizard (or *Settings → Admin →
     Dynamic DNS*, provider *Talaria name*) type a name and press *Claim*:
     you get `yourname.talaria.gg`, already pointed at your connection, with
     a token Talaria stores for you. No account, nothing to install. The
     HTTPS domain and home-access domain are filled in automatically. Names
     are served by the public talaria.gg server; see *Host names for others*
     below to run your own.
   - **Free: DuckDNS.** Create a subdomain at [duckdns.org](https://www.duckdns.org)
     and copy your token. In Talaria, *Settings → Admin*: set the HTTPS domain
     to `yourname.duckdns.org`, enable *Dynamic DNS* with provider DuckDNS and
     the token, save.
   - **Your own domain via Porkbun.** Buy the domain at
     [porkbun.com](https://porkbun.com) (DNS hosting included). In Porkbun:
     *Account → API Access* → create an API key pair **with the DNS
     permission**, and turn on *API Access* on the domain's page. In Talaria:
     HTTPS domain = your domain, Dynamic DNS provider = Porkbun with both
     keys, save. Talaria manages the DNS records itself. Leave the domain on
     Porkbun's own nameservers (no proxy or CDN in front; calls need direct
     UDP).

3. **Restart Talaria.** It keeps the DNS record pointed at your connection,
   fetches a Let's Encrypt certificate automatically, and serves
   `https://your-name`. Give friends that address.

Equivalent `talaria.toml` for a Talaria name (DuckDNS is the same with
`provider = "duckdns"`, your DuckDNS token, and no `url`):

```toml
[tls]
mode = "auto"
domain = "yourname.talaria.gg"

[ddns]
enabled = true
provider = "talaria"
url = "https://talaria.gg"
token = "tdns_…"   # from the claim; it is the only key to your name
```

**Already running on one name and switching to another?** Change the
Dynamic DNS domain first and save, wait a minute for the record to update,
then change the HTTPS domain and restart. Talaria refuses to switch to a
name that does not yet point at your server, so you cannot lock yourself out.

**Blank page from your own Wi-Fi while remote friends are fine?** Your router
cannot reach its own public address from inside (NAT loopback). Turn on *Home
network access* in Settings → Admin with a second name (`lan.yourname.talaria.gg`
for a Talaria name, another free DuckDNS subdomain, or `lan.yourdomain` on
Porkbun); Talaria points it at the
machine's LAN address with its own certificate and redirects home devices to
it. Many routers do support loopback, in which case leave this off.

**No domain at all?** `[tls] mode = "selfsigned"` serves HTTPS on your LAN
with a self-signed certificate (browsers warn once) so calls work at home.

### Host names for others

Any Talaria server with a domain at Porkbun can hand out names under it,
the way talaria.gg does. In *Settings → Admin → Hosted DDNS* turn it on,
set the zone (your domain) and, if Dynamic DNS is not already using
Porkbun, the API key pair. Other installations then enter your address as
the hosting server when they pick the *Talaria name* provider. Names are
claimed without an account (five per address per hour, reserved words and
your own hostnames excluded), each comes with a secret token that also
covers subdomains such as `lan.name`, and the list of issued names with
revoke buttons is in the same section. Records are published through
Porkbun, so they keep resolving even while your server is offline. Details
of the API are in [`docs/hosted-ddns.md`](docs/hosted-ddns.md).

Two things to know before opening it up: you will see every participating
server's public IP, and Let's Encrypt allows about 50 new certificates a
week across all names under one registered domain, which caps how many new
servers can come online per week until the zone is on the Public Suffix
List.

## Keep it running

- **Windows:** from an Administrator PowerShell in the folder:
  `.\talaria-windows-amd64.exe -service install` then
  `.\talaria-windows-amd64.exe -service start`. Talaria now starts with
  Windows and restarts itself if it crashes (`-service stop` / `-service
  uninstall` undo it). [`deploy/install.ps1`](deploy/install.ps1) does the
  same plus the firewall rule.
- **Linux (systemd):** [`deploy/install.sh`](deploy/install.sh) creates a
  `talaria` user, `/opt/talaria`, and an auto-restarting service. See
  [`docs/deploy.md`](docs/deploy.md).
- **macOS:** run it in a terminal that stays open, or wrap it in a `launchd`
  agent.

## Calls and listening together

Calls use one UDP port (50000) for everyone; the media server is inside the
same binary. Each person sends several picture sizes at once and every viewer
gets the best one their connection can carry, or whatever they pick on a
tile: Auto, high, medium, low. Microphone tiers (voice, balanced, music),
camera presets up to 4K 60 fps, screen-share presets (sharp text or smooth
motion), noise suppression and music ducking are under *Settings → Voice &
video* and in the quality button during a call. Click a person's name on
their tile to set how loud they are for you.

Listening together lives inside calls: the headphones button in a chat
starts or joins the voice call and opens the shared queue. Search YouTube in
the app or paste a link, upload songs and videos, save a queue as a
playlist. Everyone in the call is in the session; leaving the call leaves it.

## Settings and data

Every server setting is in the app under **Settings → Admin**: server name,
ports, HTTPS and dynamic DNS, home-network access, upload limits, call
bandwidth caps, YouTube and GIPHY API keys (optional; search works without
them), and users and invites. The same values live in `talaria.toml` next to
the binary and can be hand-edited while the server is stopped.

What is encrypted: all traffic (HTTPS, and DTLS-SRTP for calls), passwords
(Argon2id) and session tokens. What is not: the database and uploaded files
on disk, and messages are readable by whoever runs the server. Use full-disk
encryption on the machine if that matters to you.

## Troubleshooting

- **"Port 8080 already in use"** — change `http_port` in `talaria.toml` and
  restart; the setup page is then `http://localhost:<port>/setup`.
- **Setup page did not open** — go to `http://localhost:8080/setup` yourself.
- **Mic or camera unavailable** — browsers only allow them on HTTPS (or
  `localhost`). Set up a domain, or `mode = "selfsigned"` for the LAN.
- **Friends cannot connect / calls fail remotely** — check the three port
  forwards, that the domain resolves to your current public IP, and CGNAT.
- **Forgot the admin password** — stop Talaria and, from a source checkout,
  run `go run ./scripts/pwreset data/talaria.db <username> <new-password>`
  (point it at your real data folder), then start Talaria again.
- Logs print to the terminal; on Linux as a service, `journalctl -u talaria`.

## Development

Prerequisites: Go 1.26+ and Node 20+. Everything is pure Go (SQLite via
modernc), so cross-compiling needs no C toolchains.

```powershell
pwsh scripts/dev.ps1           # API server (:8080) + hot-reloading client (:5173); or: task dev
cd web && npm run build && cd ..          # build the web client into the binary's embed dir
go build -o dist/talaria.exe ./cmd/talaria  # or: task build
pwsh scripts/build.ps1         # cross-compile every platform; or: task build:all
go test ./... && (cd web && npm run lint)
```

Pushing a tag like `v0.2.0` makes CI publish a GitHub Release with binaries
for every platform and a `SHA256SUMS.txt`. Architecture notes are in
[`docs/architecture.md`](docs/architecture.md) and the realtime protocol in
[`docs/protocol.md`](docs/protocol.md).

## License

MIT — see [LICENSE](LICENSE).
