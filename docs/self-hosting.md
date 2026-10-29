# Self-hosting: flags and manual setup

English | [中文](self-hosting.zh-CN.md)

The simplest way is in the [README](../README.md): one command installs it and prints a
`ws://IP:8080/` address to put in Seedmux. This page is for setting it up by hand or
changing flags.

## systemd (by hand)

This is what the installer writes — no domain needed:

```ini
# /etc/systemd/system/seedmux-relay.service
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/seedmux-relay -addr :8080 -data-dir /var/lib/seedmux-relay -turn
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

```sh
systemctl daemon-reload && systemctl enable --now seedmux-relay
seedmux-relay -public-ip     # this server's public IP; the address is ws://<it>:8080/
```

For a domain with HTTPS, replace `-addr :8080` with `-domain your.domain` (ports 80 and
443 must be free).

## Flags

| Flag | Default | |
|---|---|---|
| `-addr` | `:8080` | Address and port the relay listens on; the relay address is `ws://<IP>:<port>/` |
| `-data-dir` | (memory only) | Where state is kept (computer identities, TURN secret, certificates). Required for TURN or HTTPS |
| `-turn` | off | Enable TURN on 3478 |
| `-turn-listen` | | Use another TURN address, e.g. `:3479` |
| `-turn-public-ip` | automatic | The server's public IP. If not given: looked up from `-domain`, otherwise asked from public STUN servers |
| `-turn-ports` | `50000-50999` | UDP ports for TURN relayed traffic |
| `-turn-ttl` | `12h` | Lifetime of TURN credentials |
| `-turn-max-allocations` | 16 | Concurrent TURN relays per computer |
| `-domain` | (off) | Serve HTTPS on 443 with an automatic Let's Encrypt certificate (uses 80 for the challenge). The domain must point at this server |
| `-require-desktop-key` | off | Refuse older Seedmux versions that send no identity key |
| `-max-conns-per-ip` | 64 | Concurrent connections per IP; 0 = unlimited. Mobile carriers often share one IP among many users — raise it if you have many |
| `-trust-proxy` | on | Behind a reverse proxy on the same machine, count per-IP limits by its `X-Forwarded-For` |
| `-public-ip` | | Print this server's public IP and exit |
| `-version` | | Print the version and exit |
| `-apns-*` | (off) | Official relay only: the iOS push key |

## Data directory

`-data-dir` holds:

- `desktop-keys.json`: each computer's identity. **Back it up**: if it is lost, the first
  connection claiming to be a computer can register itself as that computer.
- `turn-secret`: signs TURN credentials. Losing it only invalidates credentials already
  handed out; computers get new ones when they reconnect.
- `certs/`: HTTPS certificates with `-domain`; requested again automatically if lost.

## Firewall

Open TCP 8080 (or your port), UDP and TCP 3478, UDP 50000–50999; with a domain also TCP 80
and 443. On cloud servers also allow them in the provider's **security group** — UDP
blocked there is the most common reason TURN "is on but doesn't work".

## Checking it

- `seedmux-relay -version`: the installed version
- `curl http://<IP>:8080/health` → `{"ok":true}`
- `journalctl -u seedmux-relay` shows `TURN enabled` with the public IP it uses
- In Seedmux on iPhone, open a computer's screen over mobile data: the green line in the
  bottom-left corner shows the connection and how many frames arrive
