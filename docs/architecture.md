# Lantern architecture

## Shape

- **Single Go binary** per OS. The React client is embedded via `go:embed`
  (`internal/web`). SQLite via `modernc.org/sqlite` (pure Go → trivially
  cross-compiled with `CGO_ENABLED=0`).
- **Data layout** (next to the binary by default, `data_dir` in config):
  - `lantern.db` (+ `-wal`, `-shm`) — all relational state
  - `blobs/<sha256[0:2]>/<sha256>` — content-addressed upload storage (dedup)
  - `tmp/` — upload staging (same volume → atomic rename)
  - `certs/` — ACME cache / self-signed cert
  - `avatarcache/` — resized avatar PNGs
- **Package map** (`internal/`):
  - `config` — TOML config, portable data-dir resolution
  - `model` — plain structs shared everywhere
  - `store` — ALL SQL lives here; embedded migrations
  - `auth` — argon2id, cookie sessions, middleware
  - `api` — REST handlers + upload/file serving + router
  - `gateway` — WebSocket hub: fan-out, resume buffers, typing, time sync
  - `blob` — content-addressed file store
  - `tlsx` — autocert + self-signed
  - `web` — embedded SPA
  - Reserved for later phases: `rtc` (Pion SFU), `presence`, `syncplay`

## Key decisions

- **IDs are ULIDs** — lexicographically time-ordered, so message IDs double as
  pagination cursors (`?before=<id>`) and `MAX(id)` is "latest message".
- **Timestamps are server-side Unix ms everywhere.** Client clocks are never
  trusted; the gateway's ping/pong gives clients an offset estimate instead.
- **Messages are sent over REST, not WS** — simpler delivery guarantees and
  attachment association; idempotency via client nonce echoed in the WS event.
- **Fan-out** resolves a channel's audience per event (group members or DM
  participants); per-connection monotonic `seq` + ring buffer enables resume
  without a refetch storm on flaky mobile connections.
- **Uploads stream to disk** (never buffered in RAM), are sha256-hashed on the
  way through, MIME-sniffed server-side, and served via `http.ServeContent`
  (free Range support → `<video>`/`<audio>` seeking works).
- **Upload security**: client-declared MIME ignored; inline rendering
  allow-list (images, av media, pdf); everything else forced to
  `Content-Disposition: attachment` with `CSP: sandbox` (HTML/SVG XSS defense);
  filenames sanitized for Windows reserved names/chars.
- **channels.kind has no CHECK constraint** — Phase 2 adds `group_voice`
  without a table rebuild (SQLite can't alter constraints).
- **Migrations 0002 pre-creates Phase 2–4 tables** (calls, presence snapshots,
  provider links, playlists, playback sessions with the server-authoritative
  `position_ms` + `anchor_server_ms` clock pattern) so later work is additive.

## Phase 2 plan (voice/video) — already provisioned

- Pion SFU inside the same binary; all participants' media muxed over **one
  UDP port** (`webrtc.udp_port`, via `ice.UDPMux`); optional embedded TURN.
- 1:1 calls negotiate direct P2P (server still signals); group calls always
  SFU; simulcast for group video.
- Signaling rides the existing gateway (`rtc.*` / `call.*` reserved events).
- `getUserMedia` requires a secure context — already solved by the TLS story.

## Phase 3 plan (presence)

- Live presence is in-memory (gateway already broadcasts online/offline);
  `presence_snapshots` persists last-known state.
- "Currently playing (game)": browsers can't see processes — an optional tiny
  helper binary (`helper_tokens` table already exists for its auth) posts the
  foreground app; manual status is the fallback.
- "Currently listening" comes free from Phase 4 provider playback state.

## Phase 4 plan (synced listening / group playlists)

- Server owns the playback clock (`playback_sessions`); clients compute true
  position from their gateway clock offset.
- Provider adapters are **client-side** (Spotify Web Playback SDK — Premium
  required; Apple MusicKit JS — host needs a developer token; YouTube IFrame;
  native `<audio>`/`<video>` for uploads). The server only stores refs and
  runs the clock.
- `playlist_items.refs_json` holds per-provider track refs; `isrc` enables
  cross-service matching with title/artist/duration fuzzy fallback.
- Host retains control; `sync.host_transfer` hands it off; sessions archive
  into named `playlists`.
