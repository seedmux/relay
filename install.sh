#!/bin/sh
# Install the Seedmux relay.
#
#   ws://IP, no domain:
#     curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh
#
#   wss://domain (the domain's DNS A record must point at this server):
#     curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh -s -- --domain YOUR_DOMAIN
#
# On Linux with systemd this runs the relay as a service with TURN, opens its ports in ufw
# or firewalld if one is active, checks that it answers, and prints the address to put in
# Seedmux. Elsewhere it installs the binary only.
#
# Options:
#   --domain D       serve wss://D/ on 443 with an automatic certificate instead of ws://IP:PORT/
#   --port N         port for ws:// (default 8080)
#   --no-turn        leave TURN off (screen sharing then only works on the same network)
#   --no-firewall    don't touch ufw / firewalld
#   --no-service     install the binary only
#   --version vX.Y.Z install that release instead of the latest
#   --bin-dir DIR    where to put the binary (default /usr/local/bin)
#   --base-url URL   download from a mirror instead of GitHub releases
set -eu
LC_ALL=C
export LC_ALL

REPO="seedmux/relay"
DOCS="https://github.com/$REPO/blob/main/docs/self-hosting.md"
VERSION="latest"
PORT=8080
TURN=1
DOMAIN=""
FIREWALL=1
SERVICE=1
BIN_DIR="/usr/local/bin"
BASE_URL=""

die() { echo "seedmux-relay install: $*" >&2; exit 1; }
need() { [ $# -ge 2 ] || die "$1 needs a value"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--domain) need "$@"; DOMAIN="$2"; shift 2 ;;
	--port) need "$@"; PORT="$2"; shift 2 ;;
	--no-turn) TURN=0; shift ;;
	--no-firewall) FIREWALL=0; shift ;;
	--no-service) SERVICE=0; shift ;;
	--version) need "$@"; VERSION="$2"; shift 2 ;;
	--bin-dir) need "$@"; BIN_DIR="$2"; shift 2 ;;
	--base-url) need "$@"; BASE_URL="$2"; shift 2 ;;
	-h | --help) sed -n '2,22p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) die "unknown option: $1" ;;
	esac
done

case "$PORT" in
'' | *[!0-9]*) die "--port must be a number" ;;
esac

if [ -n "$DOMAIN" ]; then
	# Accept what people paste: https://Relay.Foo.com/ -> relay.foo.com
	DOMAIN=${DOMAIN#*://}
	DOMAIN=${DOMAIN%%/*}
	DOMAIN=$(printf '%s' "$DOMAIN" | tr '[:upper:]' '[:lower:]')
	case "$DOMAIN" in
	*[!a-z0-9.-]* | .* | *. | *..* | '') bad=1 ;;
	*.*) bad=0 ;;
	*) bad=1 ;;
	esac
	[ "$bad" = 0 ] || die "--domain $DOMAIN is not a domain name. Use your own domain whose DNS A record points at this server, or leave --domain out to use ws://IP."
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
linux | darwin | freebsd) ;;
*) die "unsupported OS: $os (download a binary from https://github.com/$REPO/releases)" ;;
esac
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported CPU: $arch" ;;
esac
asset="seedmux-relay-$os-$arch"

if [ -z "$BASE_URL" ]; then
	if [ "$VERSION" = latest ]; then
		BASE_URL="https://github.com/$REPO/releases/latest/download"
	else
		BASE_URL="https://github.com/$REPO/releases/download/$VERSION"
	fi
fi

download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "need curl or wget"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading $asset ($VERSION)..."
download "$BASE_URL/$asset" "$tmp/$asset" || die "download failed: $BASE_URL/$asset"
download "$BASE_URL/SHA256SUMS" "$tmp/SHA256SUMS" || die "download failed: $BASE_URL/SHA256SUMS"
expected=$(grep " \*\{0,1\}$asset\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
[ -n "$expected" ] || die "$asset is not listed in SHA256SUMS"
[ "$(sha256 "$tmp/$asset")" = "$expected" ] || die "checksum mismatch for $asset"

relay="$BIN_DIR/seedmux-relay"
mkdir -p "$BIN_DIR" 2>/dev/null || die "cannot create $BIN_DIR (run with sudo)"
# Copy next to it, then rename: writing over a running binary fails ("text file busy")
cp "$tmp/$asset" "$relay.new" 2>/dev/null || die "cannot write $BIN_DIR (run with sudo)"
chmod 0755 "$relay.new"
mv -f "$relay.new" "$relay"
echo "Installed $relay $("$relay" -version)"

if [ "$SERVICE" = 0 ] || [ "$os" != linux ] || ! command -v systemctl >/dev/null 2>&1; then
	echo
	if [ -n "$DOMAIN" ]; then
		echo "Start it:  $relay -domain $DOMAIN -data-dir ./seedmux-relay-data -turn"
		echo "Then put wss://$DOMAIN/ in Seedmux as the relay."
	else
		echo "Start it:  $relay -addr :$PORT -data-dir ./seedmux-relay-data -turn"
		echo "Then put ws://<this machine's public IP>:$PORT/ in Seedmux as the relay."
	fi
	exit 0
fi
[ "$(id -u)" = 0 ] || die "setting up the service needs root: run with sudo (or pass --no-service)"

ip=$("$relay" -public-ip 2>/dev/null || true)

# A port held by something other than an earlier seedmux-relay: stop before the service crash-loops
check_port() { # proto(t|u) port hint
	command -v ss >/dev/null 2>&1 || return 0
	used=$(ss -Hl"$1"np "sport = :$2" 2>/dev/null | grep -v '"seedmux-relay"' || true)
	[ -z "$used" ] && return 0
	who=$(printf '%s\n' "$used" | sed -n 's/.*users:(("\([^"]*\)".*/\1/p' | head -n 1)
	die "port $2 is already used by ${who:-another program}. $3"
}

if [ -n "$DOMAIN" ]; then
	if command -v getent >/dev/null 2>&1; then
		resolved=$(getent ahostsv4 "$DOMAIN" 2>/dev/null | awk 'NR == 1 { print $1 }')
		[ -n "$resolved" ] || die "$DOMAIN does not resolve yet. Add a DNS A record for it pointing at ${ip:-the public IP of this server}, wait until it takes effect, then run this again."
		if [ -n "$ip" ] && [ "$resolved" != "$ip" ]; then
			die "$DOMAIN points at $resolved, but this server's public IP is $ip. Change its DNS A record to $ip (with a CDN such as Cloudflare, turn its proxy off: DNS only), wait until it takes effect, then run this again."
		fi
	fi
	proxy_hint="If a web server already runs here, keep it and put the relay behind it: $DOCS#behind-nginx-or-caddy"
	check_port t 443 "$proxy_hint"
	check_port t 80 "$proxy_hint"
	flags="-domain $DOMAIN -data-dir /var/lib/seedmux-relay"
	address="wss://$DOMAIN/"
	ports="80/tcp 443/tcp"
	port_list="  80           TCP         certificate (Let's Encrypt)
  443          TCP         relay"
else
	check_port t "$PORT" "Choose another one with --port."
	flags="-addr :$PORT -data-dir /var/lib/seedmux-relay"
	address="ws://${ip:-SERVER_IP}:$PORT/"
	ports="$PORT/tcp"
	port_list="  $PORT         TCP         relay"
fi
if [ "$TURN" = 1 ]; then
	turn_hint="Free it, or install with --no-turn."
	check_port u 3478 "$turn_hint"
	check_port t 3478 "$turn_hint"
	flags="$flags -turn"
	ports="$ports 3478/udp 3478/tcp 50000-50999/udp"
	port_list="$port_list
  3478         UDP + TCP   TURN
  50000-50999  UDP         TURN relayed traffic"
fi

cat >/etc/systemd/system/seedmux-relay.service <<UNIT
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=$relay $flags
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
UNIT
systemctl daemon-reload
systemctl enable seedmux-relay >/dev/null 2>&1
systemctl restart seedmux-relay

firewall="Allow them in the firewall"
if [ "$FIREWALL" = 1 ]; then
	if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
		for p in $ports; do ufw allow "$(echo "$p" | tr - :)" >/dev/null; done
		firewall="Opened them in ufw"
	elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
		for p in $ports; do firewall-cmd --quiet --permanent --add-port="$p"; done
		firewall-cmd --quiet --reload
		firewall="Opened them in firewalld"
	fi
fi

# Wait until it answers; print its log if it doesn't come up
if command -v curl >/dev/null 2>&1; then
	if [ -n "$DOMAIN" ]; then
		up="http://127.0.0.1/"
	else
		up="http://127.0.0.1:$PORT/health"
	fi
	i=0
	until curl -sS -o /dev/null --max-time 2 "$up" 2>/dev/null; do
		i=$((i + 1))
		if [ $i -ge 10 ]; then
			journalctl -u seedmux-relay -n 15 --no-pager >&2 || true
			die "the relay did not start (log above)."
		fi
		sleep 1
	done
	# With a domain, fetch the certificate now (once: Let's Encrypt rate-limits failures).
	# It checks port 80 from outside, so this proves the domain and port 80 reach this server.
	if [ -n "$DOMAIN" ]; then
		echo "Getting the certificate for $DOMAIN..."
		if ! curl -fsS -o /dev/null --max-time 60 --resolve "$DOMAIN:443:127.0.0.1" "https://$DOMAIN/health" 2>/dev/null; then
			echo
			echo "Warning: no certificate for $DOMAIN yet. Let's Encrypt must reach this server on TCP 80:"
			echo "allow TCP 80 and 443 in your cloud provider's security group. It tries again on the next connection."
		fi
	fi
fi

cat <<DONE

The relay is running.

  Put this address in Seedmux:  $address

Ports it uses:
$port_list
$firewall. On a cloud server, also allow them in the provider's security group (web console).

Status: systemctl status seedmux-relay    Logs: journalctl -u seedmux-relay -f
DONE
