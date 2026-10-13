# Hosted dynamic DNS

A Talaria server with `[ddns_host]` enabled issues hostnames under its zone
to other Talaria installations and keeps their records published through
its own DNS provider (Porkbun). The public talaria.gg server runs this for
`*.talaria.gg`; any server with a Porkbun domain can do the same.

## Host configuration

```toml
[ddns_host]
enabled = true
zone = "talaria.gg"      # names are issued as <name>.<zone>
api_key = ""             # Porkbun key pair; defaults to the [ddns] keys
secret = ""              # when [ddns] provider is porkbun
max_names = 1000         # total cap on claimed names
```

Everything is also editable under *Settings → Admin → Hosted DDNS*, which
lists issued names with their records, last-seen time and a revoke button.
Changes apply without a restart.

Labels nobody can claim: a built-in list (`www`, `mail`, `ns1`, `admin`,
`lan`, `talaria`, …), anything starting `xn-` or containing `--`, and the
labels of the host's own HTTPS, DDNS and home-access domains inside the
zone. Claims are limited to five per client address per hour.

Records are only sent to Porkbun when they change; a client calling every
five minutes with the same IP costs one SQLite read per call. Because the
records live at Porkbun, names keep resolving while the host is offline;
only new claims and IP changes wait for it to return.

## Client configuration

```toml
[ddns]
enabled = true
provider = "talaria"
url = "https://talaria.gg"   # the hosting server
token = "tdns_…"             # from the claim
domain = "alice.talaria.gg"  # optional; defaults to tls.domain

[lan_access]
enabled = true
domain = "lan.alice.talaria.gg"  # any name beneath the claimed one
```

The token covers the claimed name and every name beneath it, so the
home-network-access domain and its DNS-01 certificate challenge use the
same credentials.

## API

All endpoints are on the host, unauthenticated except where a bearer token
is noted. Errors use the standard envelope
`{"error":{"code":"…","message":"…"}}`.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/v1/ddns/zone` | none | `{zone, server, rules}` — what names look like here. 404 when hosting is off. |
| POST | `/api/v1/ddns/claim` | none | Body `{"name":"alice"}`. Reserves the label, points its A record at the caller, returns `{name, domain, token}`. 400 malformed/reserved, 409 taken, 429 rate limited, 503 full. |
| POST | `/api/v1/ddns/update` | Bearer | Body `{"host":"", "ip":""}`. Points `host` (default: the claimed domain; may be any name beneath it) at `ip` (default: the caller's IPv4; private addresses allowed). Returns `{host, ip}`. |
| POST | `/api/v1/ddns/txt` | Bearer | Body `{"host":"", "txt":"…"}`. Publishes `_acme-challenge.<host>` TXT for Let's Encrypt DNS-01; empty `txt` clears it. 204. |
| DELETE | `/api/v1/ddns/name` | Bearer | Releases the name and removes all its records. 204. |

Provider failures (Porkbun down or rejecting) return 502 with the
provider's message; nothing is recorded as published, so the client's next
tick retries.

Admin endpoints on the host (session auth, admin only):
`GET /api/v1/admin/ddns-host/names` and
`DELETE /api/v1/admin/ddns-host/names/{name}`.

Any server (host or not) exposes `POST /api/v1/admin/claim-name` (admin) and
`POST /api/v1/setup/claim-name` (only before setup completes), which forward
a claim to a hosting server with body `{"url":"https://talaria.gg","name":"alice"}`;
an empty `name` returns the host's zone info instead.

## Operational notes

- **Certificate rate limit.** Clients fetch their own Let's Encrypt
  certificates, but all `*.<zone>` names share the registered domain's limit
  of 50 new certificates per week. Applying to add the zone to the
  [Public Suffix List](https://publicsuffix.org) lifts this (DuckDNS is
  listed for the same reason) and takes several weeks.
- **Porkbun's parked wildcard.** New Porkbun domains come with a `*` CNAME to
  the parking page. Delete it, or a name resolves to the parking page until
  it is claimed and Talaria's domain-switch guard may refuse the switch.
- **Privacy.** The host sees each client's public (and, with home access,
  LAN) IP. Say so if you open your host to strangers.
- **Tokens** are stored hashed (SHA-256). A lost token means claiming a new
  name; an admin can revoke the old one.
