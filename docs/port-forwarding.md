# Port forwarding for Talaria

For friends to reach your Talaria over the internet, your router must forward
these ports to the machine running Talaria:

| Port | Protocol | Needed for | When |
|------|----------|-----------|------|
| 80   | TCP | Let's Encrypt certificate issuance/renewal + HTTP→HTTPS redirect | Remote access |
| 443  | TCP | The app itself (HTTPS + WebSocket) | Remote access |
| 50000 | UDP | Voice/video media (SFU) | Phase 2, later |
| 3478 | UDP+TCP | TURN relay fallback | Phase 2, optional |

## Steps (any router)

1. Give your Talaria machine a **static LAN IP** (router DHCP reservation).
   Talaria prints its LAN IP on startup, e.g. `192.168.1.50`.
2. Open your router admin page — usually `http://192.168.1.1` or
   `http://192.168.0.1` (check the sticker on the router).
3. Find *Port Forwarding* (sometimes under "NAT", "Virtual Server", or
   "Gaming").
4. Add two rules pointing at your machine's LAN IP:
   - External TCP 80 → internal TCP 80
   - External TCP 443 → internal TCP 443
5. Set up DDNS so your domain follows your home IP. Many routers support
   DuckDNS/No-IP natively; otherwise run the provider's update client on the
   same machine.

## Windows Firewall

The first time Talaria runs, Windows asks to allow network access — click
**Allow**. If you missed it:

```powershell
New-NetFirewallRule -DisplayName "Talaria" -Direction Inbound -Program "C:\path\to\talaria.exe" -Action Allow
```

## Linux note

Binding ports below 1024 needs a capability:

```bash
sudo setcap 'cap_net_bind_service=+ep' ./talaria-linux-amd64
```

or forward external 80/443 to higher internal ports on the router and set
`https_port` accordingly.

## Checking for CGNAT

If `https://yourdomain` never works from outside even with correct rules:
compare the WAN IP shown in your router with the IP from a "what is my IP"
site. If they differ, your ISP uses CGNAT and inbound connections are
impossible without a tunnel (Tailscale Funnel, Cloudflare Tunnel) or a public
IP from your ISP.
