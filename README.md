# Seedmux Relay

English | [中文](README.zh-CN.md)

When your phone and your computer are on different networks, [Seedmux](https://seedmux.com)
connects them through a relay. Use the official one or run your own.

- **Forwards, can't read**: messages between phone and computer are end-to-end
  encrypted; the relay only passes them on and cannot see terminals, chats or screens.
- **TURN built in**: when the phone views the computer's screen over mobile data and a
  direct connection is impossible, the video goes through the relay.

## Quick install (Linux)

On the server:

```sh
curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh
```

It downloads the relay, installs it as a service that starts on boot, enables TURN, and
prints the relay address, for example:

```
Relay address to put in Seedmux:  ws://203.0.113.7:8080/
```

Put that address in the computer's relay setting in Seedmux, or in that computer's
`smx.toml`: `relay = "ws://203.0.113.7:8080/"`.

Open these ports in the firewall and in your cloud provider's security group:

| Port | Protocol | What |
|---|---|---|
| 8080 | TCP | Relay |
| 3478 | UDP and TCP | TURN |
| 50000–50999 | UDP | TURN relayed traffic |

Installer options (after `sudo sh -s --`): `--port 9000` to use another port,
`--no-turn` to leave TURN off.

## Build it yourself

Go 1.26 or newer:

```sh
git clone https://github.com/seedmux/relay && cd relay
go build -o seedmux-relay .
./seedmux-relay -addr :8080 -data-dir ./data -turn
```

For macOS, Windows and FreeBSD you can also download a ready-made binary from
[Releases](https://github.com/seedmux/relay/releases) and run the same command.

## Optional

- **Domain and HTTPS**: if you have a domain whose DNS points at the server, add
  `--domain your.domain` to the installer (or `-domain your.domain` to the command). The
  relay gets a certificate automatically and serves `wss://your.domain/` on 443; open TCP
  80 and 443.
- **Docker**: `docker run -d --restart unless-stopped --network host -v seedmux-relay:/data ghcr.io/seedmux/relay -addr :8080 -turn`
- **All flags and a systemd unit**: [docs/self-hosting.md](docs/self-hosting.md)

## Security

- **Computer identity**: on its first connection a computer presents its own key and the
  relay remembers it; anyone later claiming to be that computer is refused.
- **TURN can't reach private networks**: it only relays to public addresses — loopback,
  private ranges and cloud metadata addresses are refused — with a per-computer cap.
- **Abuse limits**: authenticate within 15 seconds; at most 64 connections per IP;
  pairing codes are never logged.
- **`ws://` vs `wss://`**: content is end-to-end encrypted either way. With `ws://` the
  computer's identity key crosses the network in plain text, so someone who can see your
  traffic could use it to impersonate that computer and keep it off the relay (still
  without seeing any content). If that matters, use a domain with HTTPS.

Found a vulnerability? Report it privately via **Security → Report a vulnerability** on
this repository instead of opening an issue.

## License

[AGPL-3.0](LICENSE). If you run a modified relay as a service for others, you must offer
them its source.
