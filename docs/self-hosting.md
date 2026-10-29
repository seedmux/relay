# Self-hosting the relay

English | [中文](self-hosting.zh-CN.md)

The quickest setups (one-line installer, Docker, prebuilt binaries) are in the
[README](../README.md#install). This page is for running it by hand.

## Build from source

Go 1.26 or newer.

```sh
git clone https://github.com/seedmux/relay && cd relay
go build -o seedmux-relay .
# or cross-compile for a server:
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o seedmux-relay .
```

## systemd

With automatic HTTPS (what the installer writes):

```ini
# /etc/systemd/system/seedmux-relay.service
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/seedmux-relay -domain relay.example.com -data-dir /var/lib/seedmux-relay -turn
DynamicUser=yes
StateDirectory=seedmux-relay
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
Restart=always
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

Behind your own reverse proxy on 443:

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

`<data-dir>` holds `desktop-keys.json` (registered desktops), `turn-secret` and, with
`-domain`, the HTTPS certificates in `certs/`.
Back it up: losing `desktop-keys.json` lets the next client to claim a device ID
register it; losing `turn-secret` only invalidates credentials already handed out.

## Flags

| Flag | Default | |
|---|---|---|
| `-domain` | (off) | Serve HTTPS on 443 with an automatic Let's Encrypt certificate (also listens on 80 for the challenge). The domain must point at this server |
| `-addr` | `:8080` | Plain WebSocket listen address. Behind a TLS proxy keep it on loopback; with `-domain` it is only used if given explicitly |
| `-turn` | off | Enable TURN on 3478 (shorthand for `-turn-listen :3478`) |
| `-data-dir` | (memory only) | Persistent state. Required for TURN |
| `-require-desktop-key` | off | Refuse desktops that do not present a relay key (older Seedmux versions) |
| `-max-conns-per-ip` | 64 | Concurrent connections per client IP; 0 = unlimited |
| `-trust-proxy` | on | Use `X-Forwarded-For` from a proxy on loopback for the per-IP limit |
| `-turn-listen` | (off) | TURN UDP and TCP address, e.g. `:3478` |
| `-turn-public-ip` | from `-domain` | Public IP of the server; looked up from `-domain` if not given |
| `-turn-host` | `-domain` or the IP | Host written into TURN URLs |
| `-turn-ports` | `50000-50999` | UDP range for relayed traffic |
| `-turn-ttl` | `12h` | Lifetime of TURN credentials |
| `-turn-max-allocations` | 16 | Concurrent TURN allocations per desktop |
| `-apns-*` | (off) | Official relay only: APNs key for the Seedmux iOS app |
| `-version` | | Print the version and exit |

## Firewall

Open TCP 80 and 443, UDP and TCP on the TURN port (3478), and the UDP relay
range (50000–50999). On cloud servers also allow them in the provider's security group: UDP
blocked there is the most common reason TURN "does nothing".

## Checking it

- `seedmux-relay -version` prints the installed version
- `curl https://relay.example.com/health` → `{"ok":true}`
- `curl -i https://relay.example.com/` → `426 Upgrade Required` (it only speaks WebSocket)
- In Seedmux on iPhone, open a Mac's screen over mobile data: the green debug line
  shows the connection; the relay log shows `TURN enabled` at start.
