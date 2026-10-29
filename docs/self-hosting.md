# Self-hosting: details

English | [中文](self-hosting.zh-CN.md)

The one command in the [README](../README.md) is all most people need. This page is for
seeing what the installer does, running behind an existing web server on 443, setting it
up by hand, or troubleshooting.

## What the installer does

1. Downloads the binary for your OS and CPU, verifies it against `SHA256SUMS`, and puts it
   at `/usr/local/bin/seedmux-relay`.
2. With a domain, checks that the domain points at this server and that ports 80 and 443
   are free; without one, checks port 8080. With TURN, also checks 3478.
3. Writes the systemd service `/etc/systemd/system/seedmux-relay.service` and enables it
   on boot. State lives in `/var/lib/seedmux-relay`.
4. If ufw or firewalld is active, opens the ports it needs.
5. Waits for the relay to come up; with a domain it also gets the certificate. If it fails
   to start, it prints the log.
6. Prints the address to put in Seedmux and the ports it uses.

Running it again upgrades to the latest release and rewrites the service with the options
given this time.

Options go after `sudo sh -s --`:

| Option | |
|---|---|
| `--domain YOUR_DOMAIN` | Use `wss://YOUR_DOMAIN/`; without it you get `ws://IP:8080/` |
| `--port 9000` | Another port for `ws://` (default 8080) |
| `--no-turn` | No TURN (the phone can then only view the computer's screen on the same network) |
| `--no-firewall` | Leave ufw and firewalld alone |
| `--no-service` | Install the binary only |
| `--version v0.1.0` | Install that release |

## Behind nginx or Caddy

If a website already uses 443 on the server, install the relay the default way (without
`--domain`) and let nginx or Caddy pass a domain to it. Put `wss://YOUR_DOMAIN/` in
Seedmux; port 8080 doesn't need to be open in the security group.

Caddy (gets the certificate itself):

```
YOUR_DOMAIN {
	reverse_proxy 127.0.0.1:8080
}
```

nginx (keep your existing certificate settings):

```nginx
server {
    listen 443 ssl;
    server_name YOUR_DOMAIN;
    # ssl_certificate and ssl_certificate_key as you already have them

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 1h;
    }
}
```

Keep the `X-Forwarded-For` line: without it every connection seems to come from the
server itself, and together they hit the 64-connections-per-IP limit.

## systemd by hand

The same as the installer writes. `ws://IP` mode:

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

For `wss://domain` mode, change `ExecStart` to:

```ini
ExecStart=/usr/local/bin/seedmux-relay -domain YOUR_DOMAIN -data-dir /var/lib/seedmux-relay -turn
```

Then:

```sh
systemctl daemon-reload && systemctl enable --now seedmux-relay
```

## Flags

| Flag | Default | |
|---|---|---|
| `-addr` | `:8080` | Address and port for `ws://`. With `-domain`, only listened on if given explicitly |
| `-domain` | (off) | Serve `wss://` on 443 with an automatic Let's Encrypt certificate (uses 80 for the challenge). The domain must point at this server |
| `-data-dir` | (memory only) | Where state is kept (computer identities, TURN secret, certificates). Required for TURN or a domain |
| `-turn` | off | Enable TURN on 3478 |
| `-turn-listen` | | Use another TURN address, e.g. `:3479` |
| `-turn-public-ip` | automatic | The server's public IP. If not given: looked up from `-domain`, otherwise asked from public STUN servers |
| `-turn-ports` | `50000-50999` | UDP ports for TURN relayed traffic |
| `-turn-ttl` | `12h` | Lifetime of TURN credentials |
| `-turn-max-allocations` | 16 | Concurrent TURN relays per computer |
| `-require-desktop-key` | off | Refuse older Seedmux versions that send no identity key |
| `-max-conns-per-ip` | 64 | Concurrent connections per IP; 0 = unlimited. Mobile carriers often share one IP among many users — raise it if you have many |
| `-trust-proxy` | on | Behind nginx or Caddy on the same machine, count per-IP limits by their `X-Forwarded-For` |
| `-public-ip` | | Print this server's public IP and exit |
| `-version` | | Print the version and exit |
| `-apns-*` | (off) | Official relay only: the iOS push key |

## Ports

| Port | Protocol | What | When |
|---|---|---|---|
| 8080 | TCP | Relay | `ws://IP` mode |
| 443 | TCP | Relay | `wss://domain` mode |
| 80 | TCP | Getting and renewing the certificate | `wss://domain` mode |
| 3478 | UDP and TCP | TURN | With TURN |
| 50000–50999 | UDP | TURN relayed traffic | With TURN |

## Data directory

`-data-dir` (the installer uses `/var/lib/seedmux-relay`) holds:

- `desktop-keys.json`: each computer's identity. **Back it up**: if it is lost, the first
  connection claiming to be a computer can register itself as that computer.
- `turn-secret`: signs TURN credentials. Losing it only invalidates credentials already
  handed out; computers get new ones when they reconnect.
- `certs/`: certificates with `-domain`; requested again automatically if lost.

## Troubleshooting

- **Installer says the domain doesn't point at this server**: change the A record at your
  DNS provider to the IP it shows, wait a few minutes, run it again. With a CDN, turn the
  proxy off.
- **Installer says there is no certificate yet**: Let's Encrypt must reach port 80 on this
  server from outside — allow TCP 80 and 443 in the security group. No need to reinstall;
  it tries again on the next connection.
- **The computer can't reach the relay**: open the `/health` address in a browser (see
  Check in the README). If it doesn't load, a port isn't open — check the security group
  first.
- **The phone can't view the screen over mobile data but can on Wi-Fi**: usually UDP 3478
  and UDP 50000–50999 aren't open in the security group.
- **Logs**: `journalctl -u seedmux-relay -f`. At startup a `TURN enabled` line shows the
  public IP it uses.
