# Seedmux Relay

English | [中文](README.zh-CN.md)

When your phone and your computer are on different networks, [Seedmux](https://seedmux.com)
connects them through a relay. Use the official one, or run your own on a server.

- **Forwards, can't read**: terminals, chats and screens between phone and computer are
  end-to-end encrypted; the relay only passes them on.
- **TURN built in**: when the phone views the computer's screen over mobile data and a
  direct connection is impossible, the video goes through the relay.

## Two modes

| | `ws://IP` | `wss://domain` |
|---|---|---|
| You need | A Linux server with a public IP | Plus a domain whose A record points at the server |
| Address to put in Seedmux | `ws://SERVER_IP:8080/` | `wss://DOMAIN/` |
| Relay ports | TCP 8080 | TCP 443, plus TCP 80 (for the certificate) |
| Certificate | None needed | Obtained and renewed automatically |
| Phone/computer to relay | Not encrypted (the content itself is end-to-end encrypted) | TLS |

Both modes include TURN, which also uses UDP and TCP 3478 and UDP 50000–50999.

**Which one**: no domain — use `ws://IP`; it works as soon as it is installed. Have a
domain — use `wss://domain`: port 443 is open almost everywhere, while some mobile and
office networks block or cut plain connections on ports like 8080.

## Install

Run one command on the server; it asks no questions. It installs a service that starts on
boot, turns TURN on, opens the ports in ufw or firewalld if one is running, and prints the
address to put in Seedmux.

### `ws://IP`

```sh
curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh
```

When it finishes (it finds the IP itself):

```
  Put this address in Seedmux:  ws://203.0.113.7:8080/
```

### `wss://domain`

1. At your DNS provider, add an A record for the domain pointing at the server's public IP.
   With a CDN such as Cloudflare, turn its proxy off (DNS only), or TURN won't connect.
2. Run this, replacing `YOUR_DOMAIN` at the end with your domain:

```sh
curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh -s -- --domain YOUR_DOMAIN
```

The installer first checks that the domain points at this server and stops with an
explanation if it doesn't; then it gets the certificate and prints:

```
  Put this address in Seedmux:  wss://YOUR_DOMAIN/
```

If nginx or Caddy already uses port 80 or 443, keep it and let it pass traffic to the
relay: see [Behind nginx or Caddy](docs/self-hosting.md#behind-nginx-or-caddy).

### Cloud servers: security group

Cloud providers (AWS, Tencent Cloud, Alibaba Cloud, …) have a **security group** in their
web console that the installer can't change: allow the ports above there. Don't forget
UDP — a security group without UDP is the most common reason TURN "is on but doesn't work".

## Put it in Seedmux

Everything is in Seedmux's settings on the Mac:

- **This Mac**: Settings → Phone → Relay, paste the address, press Return, then click
  **Restart**.
- **The phone**: nothing to do. It picks up the new address the next time it connects to
  the Mac; if it can no longer connect, scan the pairing code again on the phone.
- **Remote hosts managed by Seedmux** (Linux servers etc.): Settings → Remote Hosts → the
  host → Relay, fill it in and click **Switch**.

To go back to the official relay, clear the address (for a remote host, click **Use
Official Relay**).

## Check

- Open `http://SERVER_IP:8080/health` in a browser (with a domain,
  `https://YOUR_DOMAIN/health`): `{"ok":true}` means the relay is running.
- Turn off Wi-Fi on the phone and open the computer's screen in Seedmux over mobile data:
  the green line in the bottom-left corner shows the connection and how many frames arrive.

## Build it yourself

For when you have no Linux server or don't want the installer. Go 1.26 or newer:

```sh
git clone https://github.com/seedmux/relay && cd relay
go build -o seedmux-relay .
./seedmux-relay -addr :8080 -data-dir ./data -turn
```

That gives you `ws://IP:8080/`. For a domain, replace `-addr :8080` with
`-domain YOUR_DOMAIN`.

For macOS, Windows and FreeBSD you can also download a ready-made binary from
[Releases](https://github.com/seedmux/relay/releases) and use the same flags. Docker:

```sh
docker run -d --restart unless-stopped --network host -v seedmux-relay:/data ghcr.io/seedmux/relay -addr :8080 -turn
```

Manual systemd setup, all flags and troubleshooting: [docs/self-hosting.md](docs/self-hosting.md).

## Security

- **End-to-end encryption**: content between phone and computer is encrypted and
  decrypted only at the two ends; the relay, its host and the network can't read it. This
  is the same for `ws://` and `wss://`. The screen goes over WebRTC and stays encrypted when
  relayed through TURN.
- **`ws://` and `wss://` differ only on the hop to the relay**: with `ws://`, someone on
  the network can see which devices use the relay and the key the computer signs in to it
  with. With that key they could impersonate the computer and keep it off the relay, but
  not read anything. If that matters, use `wss://`.
- **Computer identity**: a computer registers its key on its first connection; anyone
  later claiming to be it is refused.
- **TURN can't reach private networks**: it only relays to public addresses — loopback,
  private ranges and cloud metadata addresses are refused — with a per-computer cap.
- **Abuse limits**: authenticate within 15 seconds; at most 64 connections per IP;
  pairing codes are never logged.

Found a vulnerability? Report it privately via **Security → Report a vulnerability** on
this repository instead of opening an issue.

## License

[AGPL-3.0](LICENSE). If you run a modified relay as a service for others, you must offer
them its source.
