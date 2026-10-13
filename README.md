# 🪽 Talaria

A portable, self-hosted chat server for friends — one binary, no install.
Think "Discord you run at home": async text chat, file sharing with inline
media playback, friends and groups, a mobile-first web client — with voice,
video, screen share, presence, and synchronized music/video listening on the
roadmap below.

## Quick start

1. Download (or build) `talaria.exe` (Windows) / `talaria-darwin-*` (macOS) /
   `talaria-linux-*` (Linux).
2. Double-click it (or run it in a terminal). Your browser opens the setup
   wizard at `http://localhost:8080`.
3. Create your admin account, then share an invite link with friends.

## Notifications and the installable app

Talaria is an installable web app: on Android and desktop Chrome/Edge use
**Settings → Notifications → Install Talaria**, on iPhone/iPad use Share →
**Add to Home Screen**. Then turn on notifications in the same Settings card
to get a heads-up for direct messages, `@mentions`, and incoming calls even
when the app is closed. Push is delivered straight from your server to the
browser (Web Push with VAPID keys generated on first run); no third-party
service is involved. Mute any chat with the bell icon in its header.

Everything lives next to the binary: `talaria.toml` (config) and `data/`
(database + uploaded files). Move the folder, and your server moves with it.
All server settings are editable in the app under **Settings → Admin — server
configuration**; the TOML file is just where they're stored, and hand-editing
it remains supported.

## Remote access (DDNS)

Friends outside your network connect through a domain pointed at your home IP:

1. Get a free DDNS domain — [DuckDNS](https://www.duckdns.org) is recommended
   (free, no expiry nags, Let's Encrypt-friendly).
2. Forward **TCP 80 and 443** (and **UDP 50000** for calls) on your router to
   the machine running Talaria (see `docs/port-forwarding.md`).
3. In **Settings → Admin — server configuration**, set the HTTPS domain and
   enable Dynamic DNS with your DuckDNS token — Talaria keeps the DDNS record
   updated itself, no separate updater client needed. (Equivalent TOML:)

   ```toml
   [tls]
   mode = "auto"
   domain = "yourname.duckdns.org"

   [ddns]
   enabled = true
   provider = "duckdns"
   token = "your-duckdns-token"   # shown at the top of duckdns.org
   ```

4. Restart Talaria. It updates DuckDNS every 5 minutes, fetches a free
   Let's Encrypt certificate automatically, and serves
   `https://yourname.duckdns.org`. (DDNS changes apply immediately on save;
   domain/port changes need the restart.)

Other providers work via `provider = "custom"` with
`url = "https://.../update?hostname={DOMAIN}&password={TOKEN}"`, or keep
using your router's built-in DDNS client and skip the `[ddns]` section.

No domain? `mode = "selfsigned"` serves HTTPS on your LAN with a self-signed
certificate (browsers warn once). Plain HTTP stays available on `:8080` for
localhost.

> **Domain not working from your own WiFi?** Many home routers can't reach
> their own public IP from inside the network (broken NAT loopback), which
> shows up as a blank page on LAN devices while everything works remotely.
> Enable **Home network access** in Settings → Admin — server configuration:
> create a second free DuckDNS subdomain (e.g. `yourname-lan`) and Talaria
> keeps it pointed at the server's LAN IP, fetches its own Let's Encrypt
> certificate (DNS-01), and transparently 302-redirects hairpinned page loads
> to it. No router or per-device changes. (A `[lan_dns]` DNS-server mode also
> exists in the TOML for setups that prefer pointing the router's DHCP DNS at
> the Talaria machine.)

> **CGNAT warning:** if your router's WAN IP differs from the IP shown by
> "what is my IP" sites, your ISP uses CGNAT and inbound connections cannot
> reach you; ask your ISP for a public IP or use a tunnel (Tailscale Funnel,
> Cloudflare Tunnel).

## Development

Prereqs: Go 1.24+, Node 20+.

```powershell
# API server (:8080) + hot-reloading client (:5173)
pwsh scripts/dev.ps1          # or: task dev

# Production build for this machine
cd web && npm run build && cd ..
go build -o dist/talaria.exe ./cmd/talaria    # or: task build

# Cross-compile all platforms (pure Go — no C toolchains needed)
pwsh scripts/build.ps1        # or: task build:all

# Tests
go test ./...
```

Architecture notes live in `docs/architecture.md`; the realtime protocol is
documented in `docs/protocol.md`.

## Roadmap

- **Phase 1 (done)** — accounts, friends, groups, invites, realtime text chat,
  replies, typing, read states, file uploads with inline image/video/audio
  playback, profiles/avatars, light+dark mobile-first UI, auto-HTTPS for DDNS.
- **Daily-driver pass (done)** — installable PWA, Web Push notifications for
  DMs/@mentions/calls, @mention autocomplete and highlighting, per-chat mute,
  unread count in the tab title and app badge, group DMs, profile cards.
- **Phase 2 (done)** — voice & video calls + screen share through a WebRTC SFU
  embedded in the same binary (Pion). Every participant uploads once; all call
  media shares **one UDP port (50000)** — forward it on your router for remote
  callers. Calls work in any group channel or DM (DMs ring the other side).
  Note: browsers require HTTPS (or localhost) to use mic/camera — LAN users
  over plain HTTP join listen-only, so enable self-signed TLS or DDNS for
  full calls. Planned refinements: direct P2P transport for 1:1 calls,
  simulcast, embedded TURN.
- **Phase 3** — presence: Currently Playing (optional tiny desktop helper) and
  Currently Listening.
- **Phase 4** — synchronized listening/watching: group playlists mixing
  YouTube, Spotify, Apple Music, and uploaded media; the host controls
  playback, everyone hears the same moment on their own service; join a
  friend's session from their presence; archive and name group playlists.
  (The schema and realtime protocol for this are already in place.)
