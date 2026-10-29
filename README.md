# Seedmux Relay

English | [中文](README.zh-CN.md)

The relay that connects the [Seedmux](https://seedmux.com) desktop app (and its
`smx` daemon) with Seedmux on iPhone when they are not on the same network.
Run your own and point your machines at it, or use the official one.

- **Forwards, never reads.** Phone and desktop pair with an X25519 key exchange and
  encrypt every message end to end (XChaCha20-Poly1305). The relay only sees the
  outer frame envelope: who talks to whom and how much. It cannot read terminals,
  chats or screens.
- **TURN built in.** When phone and desktop cannot connect directly (mobile
  networks are usually symmetric NAT), the screen-sharing WebRTC stream is relayed
  through the relay's TURN server. Credentials are short-lived and only handed to
  verified desktops.
- **Push notifications** (official relay only): the relay can forward APNs pushes
  for the Seedmux iOS app on a desktop's behalf. A self-hosted relay has no Apple
  key, so leave the `-apns-*` flags off.

## Install

You need a server with a public IP and a domain (or subdomain) whose DNS **A record** points at it.

**One line (Linux, recommended).** Installs the binary, sets up a systemd service with
automatic HTTPS (Let's Encrypt) and TURN, and starts it:

```sh
curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh -s -- --domain relay.example.com
```

**Docker.** Host networking, so the TURN port range does not have to be published:

```sh
docker run -d --name seedmux-relay --restart unless-stopped --network host \
  -v seedmux-relay:/data ghcr.io/seedmux/relay -domain relay.example.com -turn
```

**Binary.** Download `seedmux-relay-<os>-<arch>` for Linux, macOS, Windows or FreeBSD
(amd64 / arm64) from [Releases](https://github.com/seedmux/relay/releases), check it
against `SHA256SUMS`, then:

```sh
seedmux-relay -domain relay.example.com -data-dir /var/lib/seedmux-relay -turn
```

**From source** (fallback): Go 1.26 or newer, `go build -o seedmux-relay .`

Open these ports in the firewall and in your cloud provider's security group:

| Port | Protocol | What |
|---|---|---|
| 80 | TCP | Let's Encrypt certificate challenge |
| 443 | TCP | Relay (WebSocket over TLS) |
| 3478 | UDP and TCP | TURN |
| 50000–50999 | UDP | TURN relayed traffic |

Then point a machine at it: set the machine's relay in Seedmux on the Mac (host
settings), or put `relay = "wss://relay.example.com/"` in that machine's `smx.toml`.

Already running a reverse proxy (Caddy, nginx) on 443? Leave out `-domain`, listen on
loopback with `-addr 127.0.0.1:8080` behind the proxy, and give TURN the public IP with
`-turn -turn-public-ip <ip>`. See [docs/self-hosting.md](docs/self-hosting.md) for every flag.

## Security model

- **Desktop identity, trust on first use.** A desktop sends a random relay key
  with its first frame. The relay remembers its hash per device ID
  (`<data-dir>/desktop-keys.json`); later connections claiming that device ID must
  present the same key. This stops anyone from taking over a desktop's slot, its
  push quota or its TURN quota. Older desktops without a key are still accepted
  (unverified, no TURN) unless you run with `-require-desktop-key`.
- **TURN cannot reach private networks.** Allocations may only relay to public
  unicast addresses; loopback, RFC 1918, link-local (including cloud metadata),
  CGNAT, multicast and unspecified peers are refused. Each desktop has a cap on
  concurrent allocations. Credentials use the TURN REST scheme
  (`<expiry>:<device ID>`, HMAC-SHA1 with a secret in `<data-dir>/turn-secret`).
- **Abuse limits.** Connections must authenticate within 15 seconds; at most 64
  concurrent connections per client IP (behind a proxy on the same machine, the
  proxy's `X-Forwarded-For` is used); push forwarding is limited to desktops,
  60 per minute each, 4 KB per notification. Pairing tokens are never logged.

Found a vulnerability? Please report it privately through GitHub (**Security → Report a vulnerability** on this repository) instead of opening an issue.

## License

[AGPL-3.0](LICENSE). If you run a modified relay as a service for others, you
must offer them its source.
