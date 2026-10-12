# 🏮 Lantern

A portable, self-hosted chat server for friends — one binary, no install.
Think "Discord you run at home": async text chat, file sharing with inline
media playback, friends and groups, a mobile-first web client — with voice,
video, screen share, presence, and synchronized music/video listening on the
roadmap below.

## Quick start

1. Download (or build) `lantern.exe` (Windows) / `lantern-darwin-*` (macOS) /
   `lantern-linux-*` (Linux).
2. Double-click it (or run it in a terminal). Your browser opens the setup
   wizard at `http://localhost:8080`.
3. Create your admin account, then share an invite link with friends.

Everything lives next to the binary: `lantern.toml` (config) and `data/`
(database + uploaded files). Move the folder, and your server moves with it.

## Remote access (DDNS)

Friends outside your network connect through a domain pointed at your home IP:

1. Get a free DDNS domain (DuckDNS, No-IP, …) and run its updater (many
   routers have one built in).
2. Forward **TCP 80 and 443** on your router to the machine running Lantern
   (see `docs/port-forwarding.md`).
3. Enter the domain in the setup wizard — or set it later in `lantern.toml`:

   ```toml
   [tls]
   mode = "auto"
   domain = "yourname.duckdns.org"
   ```

4. Restart Lantern. It fetches a free Let's Encrypt certificate automatically
   and serves `https://yourname.duckdns.org`.

No domain? `mode = "selfsigned"` serves HTTPS on your LAN with a self-signed
certificate (browsers warn once). Plain HTTP stays available on `:8080` for
localhost.

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
go build -o dist/lantern.exe ./cmd/lantern    # or: task build

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
