# Talaria realtime protocol

One WebSocket per client at `GET /api/v1/gateway` (cookie-authenticated).
JSON text frames with this envelope:

```json
{ "op": "evt", "t": "message.create", "seq": 42, "n": "nonce", "d": { } }
```

`op` ∈ `hello | resume | resumed | reset | evt | cmd | ping | pong | err`.

## Handshake

1. Server sends `hello {session_resume_id, heartbeat_ms, server_time_ms}`.
2. Client sends `resume {session_resume_id, last_seq}` as its **first frame**.
   - Empty `session_resume_id` = fresh connect → server replies with
     `evt ready {...}` (full bootstrap state).
   - Valid resume → server sends `resumed` then replays buffered events with
     `seq > last_seq` (buffer: last 512 events, kept 5 min after disconnect).
   - Stale resume → server sends `reset`; client adopts the new resume id from
     `hello` and receives a fresh `ready` (refetch any open channel history).

## Heartbeat and clock sync

Client sends `ping {c: clientMs}` every `heartbeat_ms`; server replies
`pong {c, s: serverMs}`. Client estimates its clock offset as
`s + rtt/2 - now`, keeping a median over the last 10 samples. This offset is
the foundation for Phase 4 synchronized playback (±30 ms on LAN).

## Phase 1 events (server → client)

`ready`, `message.create|update|delete`, `channel.create|update|delete`,
`group.create|update|delete`, `group.member_add|member_remove`, `dm.create`,
`friend.request`, `friend.update`, `user.update`, `typing.start`,
`read.update`, `presence.update`.

Message sends go over REST (`POST /api/v1/channels/{id}/messages`) with a
client `nonce`; the author's own `message.create` echo carries the nonce for
optimistic-UI reconciliation.

## Phase 1 commands (client → server)

- `typing.start {channel_id}` — server throttles to 1/8s and broadcasts with a
  10 s expiry; there is no `typing.stop`.
- `read.update {channel_id, last_read_message_id}` — persists and echoes to the
  user's other devices.

## Reserved namespaces (do not reuse)

- **Phase 2**: `call.ring|state`, `call.join|leave`,
  `rtc.offer|answer|ice`, screen-share flags inside `call.state`.
- **Phase 3**: `presence.activity {type: playing|listening, name, details, track_ref}`.
- **Phase 4**: `sync.state|session_start|session_end` events;
  `sync.play|pause|seek|item_change|queue_update|host_transfer` commands.
  Every playback event carries `{position_ms, anchor_server_ms, item_id}` so
  any client computes true position as
  `position_ms + (serverNow - anchor_server_ms)` using its clock offset.
