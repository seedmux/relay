# Seedmux Relay

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

## Quick start

```sh
go build -o seedmux-relay .
./seedmux-relay -addr 127.0.0.1:8080 -data-dir ./data
```

Put a TLS reverse proxy in front of it (WebSocket upgrade passes through as is):

```caddy
relay.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

Then point a machine at it: set the machine's relay in Seedmux on the Mac (host
settings), or put `relay = "wss://relay.example.com/"` in that machine's `smx.toml`.

With TURN (so screen sharing works over mobile data):

```sh
./seedmux-relay -addr 127.0.0.1:8080 -data-dir /var/lib/seedmux-relay \
  -turn-listen :3478 -turn-public-ip 203.0.113.7 -turn-host relay.example.com
```

Open these ports to the internet:

| Port | Protocol | What |
|---|---|---|
| 443 (your proxy) | TCP | WebSocket over TLS |
| `-turn-listen` (e.g. 3478) | UDP and TCP | TURN |
| `-turn-ports` (default 50000–50999) | UDP | TURN relayed traffic |

See [docs/self-hosting.md](docs/self-hosting.md) for a systemd unit and all flags.

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
