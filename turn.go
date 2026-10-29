package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/turn/v5"
	"github.com/seedmux/relay/sdproto"
)

// Built-in TURN server: when a phone and a desktop cannot reach each other
// directly (symmetric NAT on mobile networks), WebRTC media is relayed here.
//
// Credentials are short-lived and only handed to verified desktops over their
// control connection (sdproto.MsgTurnCredentials); the desktop passes them to
// its own apps and phones. The username is "<expiry>:<desktop device ID>" and
// the password is base64(HMAC-SHA1(secret, username)) — the "TURN REST API"
// scheme — so the relay needs no per-user state to check them.
//
// Safety:
//   - allocations may only relay to public unicast addresses: loopback, private,
//     link-local, CGNAT, multicast and unspecified peers are refused, so TURN
//     cannot be used to reach the relay host's internal network
//   - each desktop has a cap on concurrent allocations

type turnConfig struct {
	// Listen is the UDP and TCP address clients connect to (e.g. ":3478")
	Listen string
	// PublicIP is the address allocations are reachable at (the relay's public IP)
	PublicIP net.IP
	// Host goes into the URLs handed to clients (a domain or the public IP)
	Host string
	// PortMin / PortMax bound the UDP ports used for allocations
	PortMin, PortMax uint16
	// Secret signs credentials
	Secret string
	// TTL of the credentials handed out
	TTL time.Duration
	// MaxAllocations per desktop
	MaxAllocations int
	Realm          string
}

type turnService struct {
	cfg    turnConfig
	server *turn.Server
	urls   []string

	mu     sync.Mutex
	allocs map[string]int // desktop device ID → live allocations
}

func startTURN(cfg turnConfig) (*turnService, error) {
	if cfg.PublicIP == nil {
		return nil, errors.New("turn: public IP required")
	}
	if cfg.Realm == "" {
		cfg.Realm = "seedmux"
	}
	if cfg.Host == "" {
		cfg.Host = cfg.PublicIP.String()
	}
	_, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("turn: listen address: %w", err)
	}
	t := &turnService{
		cfg:    cfg,
		allocs: map[string]int{},
		urls: []string{
			fmt.Sprintf("turn:%s:%s?transport=udp", hostForURL(cfg.Host), port),
			fmt.Sprintf("turn:%s:%s?transport=tcp", hostForURL(cfg.Host), port),
		},
	}

	udp, err := net.ListenPacket("udp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("turn: listen udp: %w", err)
	}
	tcp, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		udp.Close()
		return nil, fmt.Errorf("turn: listen tcp: %w", err)
	}
	relayGen := func() turn.RelayAddressGenerator {
		return &turn.RelayAddressGeneratorPortRange{
			RelayAddress: cfg.PublicIP, Address: "0.0.0.0", MinPort: cfg.PortMin, MaxPort: cfg.PortMax,
		}
	}
	server, err := turn.NewServer(turn.ServerConfig{
		Realm:        cfg.Realm,
		AuthHandler:  t.authenticate,
		QuotaHandler: t.underQuota,
		EventHandler: turn.EventHandler{
			OnAllocationCreated: func(_, _ net.Addr, _, userID, _ string, _ net.Addr, _ int) { t.countAllocation(userID, 1) },
			OnAllocationDeleted: func(_, _ net.Addr, _, userID, _ string) { t.countAllocation(userID, -1) },
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: udp, RelayAddressGenerator: relayGen(), PermissionHandler: t.permitted,
		}},
		ListenerConfigs: []turn.ListenerConfig{{
			Listener: tcp, RelayAddressGenerator: relayGen(), PermissionHandler: t.permitted,
		}},
	})
	if err != nil {
		udp.Close()
		tcp.Close()
		return nil, fmt.Errorf("turn: %w", err)
	}
	t.server = server
	return t, nil
}

func (t *turnService) Close() error { return t.server.Close() }

// turnAttrs is the request the TURN server hands to authenticate (alias, so tests can build one)
type turnAttrs = turn.RequestAttributes

// credentials for one desktop, valid for cfg.TTL
func (t *turnService) credentials(deviceID string) sdproto.TurnCredentialsPayload {
	username := fmt.Sprintf("%d:%s", time.Now().Add(t.cfg.TTL).Unix(), deviceID)
	return sdproto.TurnCredentialsPayload{
		Type:       "turn",
		URLs:       t.urls,
		Username:   username,
		Credential: turnPassword(t.cfg.Secret, username),
		TTL:        int(t.cfg.TTL / time.Second),
	}
}

func turnPassword(secret, username string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// authenticate checks "<expiry>:<device ID>" credentials; the device ID becomes the TURN user ID
func (t *turnService) authenticate(ra *turn.RequestAttributes) (string, []byte, bool) {
	expiry, deviceID, ok := parseTurnUsername(ra.Username)
	if !ok || time.Now().Unix() > expiry {
		return "", nil, false
	}
	key := turn.GenerateAuthKey(ra.Username, ra.Realm, turnPassword(t.cfg.Secret, ra.Username))
	return deviceID, key, true
}

func parseTurnUsername(username string) (expiry int64, deviceID string, ok bool) {
	stamp, id, found := strings.Cut(username, ":")
	if !found || id == "" {
		return 0, "", false
	}
	expiry, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return 0, "", false
	}
	return expiry, id, true
}

func (t *turnService) underQuota(username, _ string, _ net.Addr) bool {
	_, deviceID, ok := parseTurnUsername(username)
	if !ok {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.allocs[deviceID] >= t.cfg.MaxAllocations {
		slog.Warn("[relay:turn] allocation quota reached", "device_id", deviceID)
		return false
	}
	return true
}

func (t *turnService) countAllocation(userID string, delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.allocs[userID] += delta
	if t.allocs[userID] <= 0 {
		delete(t.allocs, userID)
	}
}

// permitted: only public unicast peers. The relay's own public IP is allowed,
// so two allocations on this relay (phone and desktop both relayed) can reach each other.
func (t *turnService) permitted(_ net.Addr, peer net.IP) bool {
	if peer.Equal(t.cfg.PublicIP) {
		return true
	}
	return publicUnicast(peer)
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func publicUnicast(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		if cgnat.Contains(v4) || v4[0] == 0 || v4.Equal(net.IPv4bcast) {
			return false
		}
	}
	return true
}

func hostForURL(host string) string {
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return "[" + host + "]"
	}
	return host
}

// loadOrCreateSecret reads the TURN secret, creating a random one on first start
func loadOrCreateSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		secret := strings.TrimSpace(string(data))
		if len(secret) < 32 {
			return "", fmt.Errorf("turn secret in %s is too short", path)
		}
		return secret, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return "", err
	}
	return secret, nil
}

func parsePortRange(value string) (uint16, uint16, error) {
	low, high, ok := strings.Cut(value, "-")
	if !ok {
		return 0, 0, fmt.Errorf("port range %q: want MIN-MAX", value)
	}
	min, err1 := strconv.ParseUint(low, 10, 16)
	max, err2 := strconv.ParseUint(high, 10, 16)
	if err1 != nil || err2 != nil || min == 0 || min > max {
		return 0, 0, fmt.Errorf("port range %q: invalid", value)
	}
	return uint16(min), uint16(max), nil
}
