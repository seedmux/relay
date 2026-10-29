#!/bin/sh
# Install the Seedmux relay.
#
#   curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh
#       just install the binary to /usr/local/bin
#   curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh -s -- --domain relay.example.com
#       also set up a systemd service: automatic HTTPS for the domain + TURN
#
# Options:
#   --domain D       set up and start a systemd service serving D (Linux, needs root)
#   --no-turn        with --domain: leave TURN off
#   --version vX.Y.Z install that release instead of the latest
#   --bin-dir DIR    where to put the binary (default /usr/local/bin)
#   --base-url URL   download from a mirror instead of GitHub releases
set -eu

REPO="seedmux/relay"
VERSION="latest"
DOMAIN=""
TURN=1
BIN_DIR="/usr/local/bin"
BASE_URL=""

die() { echo "seedmux-relay install: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
	--domain) [ $# -ge 2 ] || die "--domain needs a value"; DOMAIN="$2"; shift 2 ;;
	--no-turn) TURN=0; shift ;;
	--version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
	--bin-dir) [ $# -ge 2 ] || die "--bin-dir needs a value"; BIN_DIR="$2"; shift 2 ;;
	--base-url) [ $# -ge 2 ] || die "--base-url needs a value"; BASE_URL="$2"; shift 2 ;;
	-h | --help) sed -n '2,17p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) die "unknown option: $1" ;;
	esac
done

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

mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/$asset" "$BIN_DIR/seedmux-relay" 2>/dev/null ||
	{ cp "$tmp/$asset" "$BIN_DIR/seedmux-relay" && chmod 0755 "$BIN_DIR/seedmux-relay"; } ||
	die "cannot write $BIN_DIR (run with sudo)"
echo "Installed $BIN_DIR/seedmux-relay $("$BIN_DIR/seedmux-relay" -version)"

if [ -z "$DOMAIN" ]; then
	cat <<EOF

Run it:
  seedmux-relay -domain relay.example.com -data-dir /var/lib/seedmux-relay -turn
or rerun this installer with --domain to set it up as a service.
EOF
	exit 0
fi

[ "$os" = linux ] || die "--domain sets up a systemd service, which needs Linux"
command -v systemctl >/dev/null 2>&1 || die "--domain needs systemd"
[ "$(id -u)" = 0 ] || die "--domain needs root (run with sudo)"

flags="-domain $DOMAIN -data-dir /var/lib/seedmux-relay"
[ "$TURN" = 1 ] && flags="$flags -turn"

cat >/etc/systemd/system/seedmux-relay.service <<EOF
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=$BIN_DIR/seedmux-relay $flags
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
EOF
systemctl daemon-reload
systemctl enable --now seedmux-relay >/dev/null
systemctl restart seedmux-relay

turn_ports=""
[ "$TURN" = 1 ] && turn_ports="
  3478         UDP + TCP   TURN
  50000-50999  UDP         TURN relayed traffic"
cat <<EOF

Seedmux relay is running as a service (systemctl status seedmux-relay).
  - $DOMAIN must resolve to this server; the certificate is requested on first connection.
  - Open these ports in the firewall / cloud security group:
  80           TCP         certificate challenge
  443          TCP         relay (WebSocket over TLS)$turn_ports
  - Point a machine at it: relay = "wss://$DOMAIN/" in its smx.toml, or set it in Seedmux on the Mac.
  - Logs: journalctl -u seedmux-relay -f
EOF
