#!/bin/sh
# Install the Seedmux relay.
#
#   curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh
#
# On Linux with systemd this installs the binary and runs it as a service on port 8080
# with TURN, then prints the address to put in Seedmux (ws://<this server's IP>:8080/).
# Elsewhere it installs the binary only.
#
# Options:
#   --port N         port for the relay (default 8080)
#   --no-turn        leave TURN off (screen sharing then only works on the same network)
#   --domain D       optional: serve HTTPS on 443 for domain D (its DNS A record must point here)
#                    instead of plain ws:// on --port
#   --no-service     install the binary only
#   --version vX.Y.Z install that release instead of the latest
#   --bin-dir DIR    where to put the binary (default /usr/local/bin)
#   --base-url URL   download from a mirror instead of GitHub releases
set -eu

REPO="seedmux/relay"
VERSION="latest"
PORT=8080
TURN=1
DOMAIN=""
SERVICE=1
BIN_DIR="/usr/local/bin"
BASE_URL=""

die() { echo "seedmux-relay install: $*" >&2; exit 1; }
need() { [ $# -ge 2 ] || die "$1 needs a value"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--port) need "$@"; PORT="$2"; shift 2 ;;
	--no-turn) TURN=0; shift ;;
	--domain) need "$@"; DOMAIN="$2"; shift 2 ;;
	--no-service) SERVICE=0; shift ;;
	--version) need "$@"; VERSION="$2"; shift 2 ;;
	--bin-dir) need "$@"; BIN_DIR="$2"; shift 2 ;;
	--base-url) need "$@"; BASE_URL="$2"; shift 2 ;;
	-h | --help) sed -n '2,20p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) die "unknown option: $1" ;;
	esac
done

case "$PORT" in
'' | *[!0-9]*) die "--port must be a number" ;;
esac

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

mkdir -p "$BIN_DIR" 2>/dev/null || die "cannot create $BIN_DIR (run with sudo)"
cp "$tmp/$asset" "$BIN_DIR/seedmux-relay" 2>/dev/null || die "cannot write $BIN_DIR (run with sudo)"
chmod 0755 "$BIN_DIR/seedmux-relay"
relay="$BIN_DIR/seedmux-relay"
echo "Installed $relay $("$relay" -version)"

if [ "$SERVICE" = 0 ] || [ "$os" != linux ] || ! command -v systemctl >/dev/null 2>&1; then
	echo
	echo "Start it:  $relay -addr :$PORT -data-dir ./seedmux-relay-data -turn"
	echo "Then put ws://<this machine's IP>:$PORT/ in Seedmux as the relay."
	exit 0
fi
[ "$(id -u)" = 0 ] || die "setting up the service needs root: run with sudo (or pass --no-service)"

if [ -n "$DOMAIN" ]; then
	flags="-domain $DOMAIN -data-dir /var/lib/seedmux-relay"
	address="wss://$DOMAIN/"
	web_ports="  80           TCP         HTTPS certificate
  443          TCP         relay"
else
	flags="-addr :$PORT -data-dir /var/lib/seedmux-relay"
	ip=$("$relay" -public-ip 2>/dev/null || true)
	address="ws://${ip:-SERVER_IP}:$PORT/"
	web_ports="  $PORT         TCP         relay"
fi
turn_ports=""
if [ "$TURN" = 1 ]; then
	flags="$flags -turn"
	turn_ports="
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

cat <<DONE

The relay is running (systemctl status seedmux-relay, logs: journalctl -u seedmux-relay -f).

Relay address to put in Seedmux:  $address

Open these ports in the firewall and in your cloud provider's security group:
$web_ports$turn_ports
DONE
