# Self-hosting the relay

## Build

Go 1.24 or newer.

```sh
git clone https://github.com/seedmux/relay && cd relay
go build -o seedmux-relay .
# or cross-compile for a server:
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o seedmux-relay .
```

## systemd

```ini
# /etc/systemd/system/seedmux-relay.service
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/seedmux-relay -addr 127.0.0.1:8080 -data-dir /var/lib/seedmux-relay \
  -turn-listen :3478 -turn-public-ip 203.0.113.7 -turn-host relay.example.com
DynamicUser=yes
StateDirectory=seedmux-relay
Restart=always
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

```sh
systemctl daemon-reload && systemctl enable --now seedmux-relay
```

`<data-dir>` holds `desktop-keys.json` (registered desktops) and `turn-secret`.
Back it up: losing `desktop-keys.json` lets the next client to claim a device ID
register it; losing `turn-secret` only invalidates credentials already handed out.

## Flags

| Flag | Default | |
|---|---|---|
| `-addr` | `:8080` | WebSocket listen address. Keep it on loopback behind a TLS proxy |
| `-data-dir` | (memory only) | Persistent state. Required for TURN |
| `-require-desktop-key` | off | Refuse desktops that do not present a relay key (older Seedmux versions) |
| `-max-conns-per-ip` | 64 | Concurrent connections per client IP; 0 = unlimited |
| `-trust-proxy` | on | Use `X-Forwarded-For` from a proxy on loopback for the per-IP limit |
| `-turn-listen` | (off) | TURN UDP and TCP address, e.g. `:3478` |
| `-turn-public-ip` | | Public IP of the server (required with TURN) |
| `-turn-host` | public IP | Host written into TURN URLs |
| `-turn-ports` | `50000-50999` | UDP range for relayed traffic |
| `-turn-ttl` | `12h` | Lifetime of TURN credentials |
| `-turn-max-allocations` | 16 | Concurrent TURN allocations per desktop |
| `-apns-*` | (off) | Official relay only: APNs key for the Seedmux iOS app |

## Firewall

Open TCP 443 (your TLS proxy), UDP and TCP on the TURN port, and the UDP relay
range. On cloud servers also allow them in the provider's security group: UDP
blocked there is the most common reason TURN "does nothing".

## Checking it

- `curl https://relay.example.com/health` → `{"ok":true}`
- `curl -i https://relay.example.com/` → `426 Upgrade Required` (it only speaks WebSocket)
- In Seedmux on iPhone, open a Mac's screen over mobile data: the green debug line
  shows the connection; the relay log shows `TURN enabled` at start.
