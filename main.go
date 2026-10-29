package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/seedmux/relay/internal/apnsclient"
	"github.com/seedmux/relay/sdproto"
	"golang.org/x/crypto/acme/autocert"
)

func fmtRelayBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.2fMB", float64(n)/1024/1024)
}

type peer struct {
	deviceID       string
	conn           *websocket.Conn
	role           string // "desktop" | "mobile"
	token          string
	isControl      bool  // true if this is a desktop control connection
	peer           *peer // paired mobile peer (for data connections)
	pairedDeviceID string
	// verified: a desktop that proved its relay key (identity.go). Only verified
	// desktops get TURN credentials; with -require-desktop-key only they may connect.
	verified bool
}

type relay struct {
	mu sync.Mutex
	// deviceID → connected peer set
	peers map[string]map[*peer]struct{}
	// desktopCtrl tracks the current control connection per desktop deviceID
	desktopCtrl map[string]*peer
	// pendingBind tracks the exact mobile peer awaiting a bind, keyed by "desktopID:mobileID"
	pendingBind map[string]*peer
	// token → peers waiting with that token
	waiting map[string][]*peer

	// APNs client; nil when relay runs without --apns-* flags (push disabled).
	// Guarded by atomic initialization at startup, not by r.mu.
	apns *apnsclient.Client

	opts      relayOptions
	keys      *desktopKeys
	turn      *turnService // nil = TURN off
	conns     *connLimiter
	apnsLimit *rateLimiter
}

// relayOptions are the safety knobs (flags in main).
type relayOptions struct {
	// RequireDesktopKey rejects desktops that do not present a relay key
	RequireDesktopKey bool
	// MaxConnsPerIP caps concurrent WebSocket connections from one IP (0 = no cap)
	MaxConnsPerIP int
	// TrustProxy honors X-Forwarded-For from a reverse proxy on loopback
	TrustProxy bool
	// AuthTimeout: a connection must send its first auth / reconnect frame within this
	AuthTimeout time.Duration
	// MaxWaitingPerToken caps desktops waiting on one pairing token
	MaxWaitingPerToken int
}

func defaultOptions() relayOptions {
	return relayOptions{MaxConnsPerIP: 64, TrustProxy: true, AuthTimeout: 15 * time.Second, MaxWaitingPerToken: 4}
}

const maxFrameSize = 32 * 1024 * 1024 // 32MB

// version is set at build time (-ldflags "-X main.version=v1.2.3")
var version = "dev"

// maxApnsPayload: Apple rejects notification payloads above 4 KB anyway
const maxApnsPayload = 4096

func newRelay() *relay {
	keys, _ := loadDesktopKeys("")
	return newRelayWith(defaultOptions(), keys, nil)
}

func newRelayWith(opts relayOptions, keys *desktopKeys, turn *turnService) *relay {
	return &relay{
		peers:       make(map[string]map[*peer]struct{}),
		desktopCtrl: make(map[string]*peer),
		pendingBind: make(map[string]*peer),
		waiting:     make(map[string][]*peer),
		opts:        opts,
		keys:        keys,
		turn:        turn,
		conns:       newConnLimiter(opts.MaxConnsPerIP),
		apnsLimit:   newRateLimiter(60, 20),
	}
}

// JSON payloads carried inside SD control frames.
type authMsg struct {
	Type         string `json:"type"`
	Token        string `json:"token"`
	Role         string `json:"role"`
	DeviceID     string `json:"deviceId"`
	PeerDeviceID string `json:"peerDeviceId,omitempty"`
	// relay doesn't use these fields, but forwards them to the peer.
	PublicKey string `json:"publicKey,omitempty"`
	Proof     string `json:"proof,omitempty"`
	// RelayKey: the desktop's secret for this relay (identity.go); desktops only
	RelayKey string `json:"relayKey,omitempty"`
}

type reconnectMsg struct {
	Type         string `json:"type"`
	DeviceID     string `json:"deviceId"`
	PeerDeviceID string `json:"peerDeviceId"`
	Salt         string `json:"salt,omitempty"`
	Ts           int64  `json:"ts,omitempty"`
	AuthTag      string `json:"authTag,omitempty"`
}

func sendSDJSON(ctx context.Context, conn *websocket.Conn, msgType byte, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, sdproto.Pack(msgType, 0, data))
}

func sendProtocolError(ctx context.Context, conn *websocket.Conn, message string) error {
	return sendSDJSON(ctx, conn, sdproto.MsgError, map[string]string{
		"type":    "error",
		"message": message,
	})
}

func sendWaiting(ctx context.Context, conn *websocket.Conn) error {
	return sendSDJSON(ctx, conn, sdproto.MsgError, map[string]string{
		"type": "waiting",
	})
}

func (r *relay) handleConn(ctx context.Context, conn *websocket.Conn) {
	var me *peer
	defer func() {
		if me != nil {
			r.removePeer(ctx, me)
		}
	}()

	authDeadline := time.Now().Add(r.opts.AuthTimeout)
	for {
		readCtx, cancelRead := ctx, context.CancelFunc(func() {})
		if me == nil && r.opts.AuthTimeout > 0 {
			// Not authenticated yet: don't let an idle socket hold a slot
			readCtx, cancelRead = context.WithDeadline(ctx, authDeadline)
		}
		msgType, data, err := conn.Read(readCtx)
		cancelRead()
		if err != nil {
			return
		}

		if msgType != websocket.MessageBinary {
			slog.Warn("[relay] rejected non-binary message", "type", msgType)
			if err := sendProtocolError(ctx, conn, "expected binary SD frame"); err != nil {
				return
			}
			continue
		}

		frame, err := sdproto.Unpack(data)
		if err != nil {
			slog.Warn("[relay] invalid SD frame", "err", err)
			if err := sendProtocolError(ctx, conn, "invalid SD frame"); err != nil {
				return
			}
			continue
		}

		switch frame.MsgType {
		case sdproto.MsgAuth:
			var msg authMsg
			if err := json.Unmarshal(frame.Payload, &msg); err != nil {
				slog.Warn("[relay] auth decode failed", "err", err)
				if err := sendProtocolError(ctx, conn, "invalid auth payload"); err != nil {
					return
				}
				continue
			}

			// Handle "bind" — desktop opening a data connection for a reconnected device
			if msg.Type == "bind" {
				if _, err := r.keys.check(msg.DeviceID, msg.RelayKey); err != nil || (r.opts.RequireDesktopKey && msg.RelayKey == "") {
					slog.Warn("[relay] bind rejected: desktop identity", "device_id", msg.DeviceID, "err", err)
					_ = sendProtocolError(ctx, conn, "desktop identity check failed")
					return
				}
				r.handleBind(ctx, conn, msg.DeviceID, msg.PeerDeviceID)
				return
			}

			if msg.Token == "" || msg.DeviceID == "" || (msg.Role != "desktop" && msg.Role != "mobile") {
				if err := sendProtocolError(ctx, conn, "invalid auth fields"); err != nil {
					return
				}
				continue
			}

			verified := false
			if msg.Role == "desktop" {
				ok, err := r.keys.check(msg.DeviceID, msg.RelayKey)
				if err == nil && r.opts.RequireDesktopKey && !ok {
					err = errDesktopKeyRequired
				}
				if err != nil {
					slog.Warn("[relay] desktop rejected", "device_id", msg.DeviceID, "err", err)
					_ = sendSDJSON(ctx, conn, sdproto.MsgError, map[string]string{
						"type": "error", "code": "desktop_identity", "message": err.Error(),
					})
					return
				}
				verified = ok
			}

			me = &peer{
				deviceID: msg.DeviceID,
				conn:     conn,
				role:     msg.Role,
				token:    msg.Token,
				verified: verified,
			}

			if msg.Role == "desktop" {
				me.isControl = true
				r.mu.Lock()
				r.indexPeerLocked(me)
				r.desktopCtrl[msg.DeviceID] = me
				r.mu.Unlock()
			}

			if err := r.addPeer(ctx, me, frame.Payload); err != nil {
				slog.Error("[relay] add peer failed", "device_id", msg.DeviceID, "err", err)
				return
			}
			if me.isControl && me.verified && r.turn != nil {
				go r.keepTurnCredentials(ctx, me)
			}

		case sdproto.MsgReconnect:
			var msg reconnectMsg
			if err := json.Unmarshal(frame.Payload, &msg); err != nil {
				slog.Warn("[relay] reconnect decode failed", "err", err)
				if err := sendProtocolError(ctx, conn, "invalid reconnect payload"); err != nil {
					return
				}
				continue
			}
			if msg.DeviceID == "" || msg.PeerDeviceID == "" {
				if err := sendProtocolError(ctx, conn, "invalid reconnect fields"); err != nil {
					return
				}
				continue
			}

			if me == nil {
				me = &peer{
					deviceID: msg.DeviceID,
					conn:     conn,
					role:     "mobile",
				}
				r.mu.Lock()
				r.indexPeerLocked(me)
				r.mu.Unlock()
			}

			if err := r.handleReconnect(ctx, me, &msg, data); err != nil {
				slog.Error("[relay] reconnect failed", "device_id", msg.DeviceID, "peer_device_id", msg.PeerDeviceID, "err", err)
				return
			}

		case sdproto.MsgApnsPush:
			// Desktop → Relay: deliver APNs notification on Desktop's behalf.
			// Runs in a goroutine so the Apple HTTP/2 RTT (~100ms typical)
			// doesn't block the Desktop's read loop or other Desktop frames.
			// Only desktops may push (not phones, not unauthenticated sockets), at a
			// bounded rate and size: the APNs key is shared by every user of this relay.
			if me == nil || me.role != "desktop" {
				_ = sendProtocolError(ctx, conn, "apns push: desktops only")
				continue
			}
			if len(frame.Payload) > maxApnsPayload+1024 {
				r.respondApnsError(ctx, me, frame.Payload, 0, "payload_too_large")
				continue
			}
			if !r.apnsLimit.allow(me.deviceID) {
				r.respondApnsError(ctx, me, frame.Payload, 0, "rate_limited")
				continue
			}
			go r.handleApnsPush(ctx, me, frame.Payload)

		case sdproto.MsgTurnRequest:
			if r.turn == nil || me == nil || !me.isControl || !me.verified {
				_ = sendProtocolError(ctx, conn, "turn: not available")
				continue
			}
			if err := sendSDJSON(ctx, conn, sdproto.MsgTurnCredentials, r.turn.credentials(me.deviceID)); err != nil {
				return
			}

		default:
			if err := r.forwardToPeer(ctx, me, data); err != nil {
				slog.Warn("[relay] forward failed", "msg_type", fmt.Sprintf("0x%02x", frame.MsgType), "err", err)
				if err := sendProtocolError(ctx, conn, err.Error()); err != nil {
					return
				}
			}
		}
	}
}

func (r *relay) forwardToPeer(ctx context.Context, me *peer, data []byte) error {
	if me == nil || me.peer == nil {
		return fmt.Errorf("not paired")
	}

	r.mu.Lock()
	target := me.peer
	// Use data connection if available, otherwise fall back to peer's main conn
	var targetConn *websocket.Conn
	if target != nil {
		targetConn = target.conn
	}
	r.mu.Unlock()
	if targetConn == nil {
		return fmt.Errorf("peer offline")
	}

	start := time.Now()
	if err := targetConn.Write(ctx, websocket.MessageBinary, data); err != nil {
		return fmt.Errorf("write to %s: %w", target.deviceID, err)
	}
	writeMs := time.Since(start).Seconds() * 1000
	slog.Debug("[relay:perf] forward",
		"from", me.deviceID,
		"to", target.deviceID,
		"size", fmtRelayBytes(len(data)),
		"write_ms", writeMs)
	return nil
}

func (r *relay) addPeer(ctx context.Context, p *peer, rawAuth []byte) error {
	r.mu.Lock()

	r.indexPeerLocked(p)

	waitList := r.waiting[p.token]
	var matched *peer
	matchedIndex := -1
	for i, w := range waitList {
		if w == nil || w == p || w.deviceID == p.deviceID || w.role == p.role || w.peer != nil || w.pairedDeviceID != "" {
			continue
		}
		p.peer = w
		w.peer = p
		p.pairedDeviceID = w.deviceID
		w.pairedDeviceID = p.deviceID
		matched = w
		matchedIndex = i
		break
	}
	if matched == nil && p.role == "desktop" && r.opts.MaxWaitingPerToken > 0 && len(waitList) >= r.opts.MaxWaitingPerToken {
		r.mu.Unlock()
		_ = sendProtocolError(ctx, p.conn, "too many desktops waiting on this pairing token")
		return fmt.Errorf("waiting list full")
	}
	if matched == nil {
		// mobile role 没匹配到 desktop:不进 waiting,立刻告知失败。
		// 没有合法场景需要 mobile 等 desktop —— QR 是 desktop 生成的,没 desktop
		// 就没 QR。mobile 拿过期 / 已用 / 老截图 QR 来时,desktop 在 relay 这边
		// 早就用新 token 重新 register 了,在 waiting[oldToken] 里找不到。
		// 让 mobile 立刻拿到 error 比干等 30s timeout 体验好得多。
		if p.role == "mobile" {
			r.mu.Unlock()
			// The pairing token is a secret: never log it
			slog.Warn("[relay] no desktop for mobile, rejecting", "device_id", p.deviceID)
			_ = sendSDJSON(ctx, p.conn, sdproto.MsgError, map[string]string{
				"type":    "error",
				"code":    "no_desktop",
				"message": "no desktop registered for this QR token. The QR may have expired or been used. Please scan a fresh QR.",
			})
			return nil
		}
		// desktop role:正常进 waiting 等 mobile 扫码到来。
		r.waiting[p.token] = append(waitList, p)
		r.mu.Unlock()
		return nil
	}
	r.waiting[p.token] = append(waitList[:matchedIndex], waitList[matchedIndex+1:]...)
	if len(r.waiting[p.token]) == 0 {
		delete(r.waiting, p.token)
	}
	r.mu.Unlock()

	// Notify desktop (control conn) with mobile's info
	if err := sendSDJSON(ctx, matched.conn, sdproto.MsgPaired, map[string]interface{}{
		"type":          "paired",
		"peerDeviceId":  p.deviceID,
		"peerPublicKey": jsonField(rawAuth, "publicKey"),
		"peerProof":     jsonField(rawAuth, "proof"),
	}); err != nil {
		return fmt.Errorf("notify desktop paired: %w", err)
	}

	// Notify mobile
	if err := sendSDJSON(ctx, p.conn, sdproto.MsgPaired, map[string]interface{}{
		"type":         "paired",
		"peerDeviceId": matched.deviceID,
	}); err != nil {
		return fmt.Errorf("notify mobile paired: %w", err)
	}
	return nil
}

// handleReconnect forwards reconnect to the desktop's CONTROL connection (not old data connection).
func (r *relay) handleReconnect(ctx context.Context, me *peer, msg *reconnectMsg, rawData []byte) error {
	r.mu.Lock()

	// Find desktop's control connection
	ctrl := r.desktopCtrl[msg.PeerDeviceID]
	if ctrl == nil {
		r.mu.Unlock()
		return sendWaiting(ctx, me.conn)
	}

	// Track the pairing for disconnect notification and precise bind matching
	me.pairedDeviceID = ctrl.deviceID
	bindKey := ctrl.deviceID + ":" + me.deviceID
	r.pendingBind[bindKey] = me

	r.mu.Unlock()

	// Forward reconnect to desktop's CONTROL connection
	if err := ctrl.conn.Write(ctx, websocket.MessageBinary, rawData); err != nil {
		return fmt.Errorf("forward reconnect to %s: %w", ctrl.deviceID, err)
	}
	return nil
}

// handleBind associates a new data connection from desktop with a previously reconnected mobile peer.
func (r *relay) handleBind(ctx context.Context, dataConn *websocket.Conn, desktopID, mobileID string) {
	r.mu.Lock()

	// Look up the exact mobile peer that triggered this reconnect.
	// Using pendingBind avoids matching a stale old connection with the same pairedDeviceID.
	bindKey := desktopID + ":" + mobileID
	mobilePeer := r.pendingBind[bindKey]
	delete(r.pendingBind, bindKey)
	if mobilePeer == nil {
		r.mu.Unlock()
		slog.Warn("[relay] bind rejected, no pending mobile peer", "desktop_id", desktopID, "mobile_id", mobileID)
		return
	}

	// Create a data peer for the desktop side
	dataPeer := &peer{
		deviceID:       desktopID,
		conn:           dataConn,
		role:           "desktop",
		pairedDeviceID: mobileID,
		peer:           mobilePeer,
	}
	mobilePeer.peer = dataPeer
	r.indexPeerLocked(dataPeer)
	r.mu.Unlock()

	slog.Info("[relay] bind established", "desktop_id", desktopID, "mobile_id", mobileID)

	// Forward data from desktop data conn → mobile (blocking loop)
	defer func() {
		r.mu.Lock()
		r.unindexPeerLocked(dataPeer)
		if mobilePeer.peer == dataPeer {
			mobilePeer.peer = nil
		}
		r.mu.Unlock()
	}()

	for {
		_, data, err := dataConn.Read(ctx)
		if err != nil {
			return
		}
		if mobilePeer.conn != nil {
			mobilePeer.conn.Write(ctx, websocket.MessageBinary, data)
		}
	}
}

func (r *relay) removePeer(ctx context.Context, p *peer) {
	r.mu.Lock()
	r.unindexPeerLocked(p)

	// Clean up control connection tracking
	if p.isControl {
		if r.desktopCtrl[p.deviceID] == p {
			delete(r.desktopCtrl, p.deviceID)
		}
	}

	// Clean up peer linkage and close paired data conn (but not the active control conn)
	if p.peer != nil && p.peer.peer == p {
		paired := p.peer
		if paired.conn != nil && r.desktopCtrl[paired.deviceID] != paired {
			paired.conn.CloseNow() // closes desktop data conn → gateway ReadLoop exits
		}
		paired.peer = nil
		paired.pairedDeviceID = ""
	}

	// If mobile disconnects, clean up pendingBind and notify desktop
	var notifyDeviceID string
	if p.role == "mobile" && p.pairedDeviceID != "" {
		notifyDeviceID = p.pairedDeviceID
		// Clean stale pendingBind so a late bind doesn't attach to this dead peer
		bindKey := p.pairedDeviceID + ":" + p.deviceID
		delete(r.pendingBind, bindKey)
	}

	for token, list := range r.waiting {
		for i, candidate := range list {
			if candidate == p {
				r.waiting[token] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(r.waiting[token]) == 0 {
			delete(r.waiting, token)
		}
	}

	ctrl := r.desktopCtrl[notifyDeviceID]
	r.mu.Unlock()

	// Send peer_disconnected to desktop's CONTROL connection
	if ctrl != nil {
		if err := sendSDJSON(ctx, ctrl.conn, sdproto.MsgPeerDisconnected, map[string]string{
			"type":         "peer_disconnected",
			"peerDeviceId": p.deviceID,
		}); err != nil {
			slog.Warn("[relay] notify peer_disconnected failed", "device_id", notifyDeviceID, "err", err)
		}
	}
}

func (r *relay) indexPeerLocked(p *peer) {
	if r.peers[p.deviceID] == nil {
		r.peers[p.deviceID] = make(map[*peer]struct{})
	}
	r.peers[p.deviceID][p] = struct{}{}
}

func (r *relay) unindexPeerLocked(p *peer) {
	set := r.peers[p.deviceID]
	if set == nil {
		return
	}
	delete(set, p)
	if len(set) == 0 {
		delete(r.peers, p.deviceID)
	}
}

func jsonField(data []byte, field string) string {
	if data == nil || field == "" {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	v, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

func setupMux(r *relay) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true}`)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		ip := clientIP(req, r.opts.TrustProxy)
		if !r.conns.acquire(ip) {
			http.Error(w, "too many connections", http.StatusTooManyRequests)
			return
		}
		defer r.conns.release(ip)
		conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		conn.SetReadLimit(maxFrameSize)
		defer conn.CloseNow()

		ctx, cancel := context.WithTimeout(req.Context(), 24*time.Hour)
		defer cancel()

		r.handleConn(ctx, conn)
	})

	return mux
}

func main() {
	addr := flag.String("addr", ":9876", "plain HTTP listen address (with -domain: only used if given explicitly)")
	domain := flag.String("domain", "", "serve HTTPS on :443 with an automatic Let's Encrypt certificate for this domain (also listens on :80 for the certificate challenge)")
	turnOn := flag.Bool("turn", false, "enable TURN on :3478 (shorthand for -turn-listen :3478)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	showPublicIP := flag.Bool("public-ip", false, "print this server's public IP (as seen by public STUN servers) and exit")
	apnsKeyPath := flag.String("apns-key-path", "", "path to APNs .p8 auth key (enables APNs push forwarding)")
	apnsKeyID := flag.String("apns-key-id", "", "APNs Key ID (from Apple Developer)")
	apnsTeamID := flag.String("apns-team-id", "", "Apple Team ID")
	apnsBundleID := flag.String("apns-bundle-id", "", "iOS app bundle ID (e.g., com.example.seedmux.ios)")
	dataDir := flag.String("data-dir", "", "directory for persistent state (desktop keys, TURN secret); empty = in memory only")
	requireKey := flag.Bool("require-desktop-key", false, "reject desktops that do not present a relay key")
	maxConns := flag.Int("max-conns-per-ip", 64, "concurrent WebSocket connections allowed per client IP (0 = unlimited)")
	trustProxy := flag.Bool("trust-proxy", true, "use X-Forwarded-For from a reverse proxy on loopback for per-IP limits")
	turnListen := flag.String("turn-listen", "", "TURN UDP+TCP listen address, e.g. :3478 (empty = TURN off)")
	turnPublicIP := flag.String("turn-public-ip", "", "public IP of this relay (required with -turn-listen)")
	turnHost := flag.String("turn-host", "", "host put in TURN URLs (domain or IP; default: public IP)")
	turnPorts := flag.String("turn-ports", "50000-50999", "UDP port range for TURN allocations")
	turnTTL := flag.Duration("turn-ttl", 12*time.Hour, "lifetime of TURN credentials")
	turnMaxAllocs := flag.Int("turn-max-allocations", 16, "concurrent TURN allocations per desktop")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *showPublicIP {
		ip, err := detectPublicIP()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(ip)
		return
	}
	if placeholderDomain(*domain) {
		log.Fatalf("[relay] -domain %s is an example: use a domain you own whose DNS A record points at this server, or leave -domain out and use ws://<ip>:<port>", *domain)
	}
	addrGiven := false
	flag.Visit(func(f *flag.Flag) { addrGiven = addrGiven || f.Name == "addr" })
	if *turnOn && *turnListen == "" {
		*turnListen = ":3478"
	}
	if *domain != "" && *dataDir == "" {
		log.Fatal("[relay] -domain needs -data-dir (certificates are kept there)")
	}

	opts := defaultOptions()
	opts.RequireDesktopKey = *requireKey
	opts.MaxConnsPerIP = *maxConns
	opts.TrustProxy = *trustProxy

	keyPath := ""
	if *dataDir != "" {
		keyPath = filepath.Join(*dataDir, "desktop-keys.json")
	}
	keys, err := loadDesktopKeys(keyPath)
	if err != nil {
		log.Fatalf("[relay] load desktop keys: %v", err)
	}

	var turnSvc *turnService
	if *turnListen != "" {
		if *dataDir == "" {
			log.Fatal("[relay] -turn-listen needs -data-dir (the TURN secret must survive restarts)")
		}
		ip := net.ParseIP(*turnPublicIP)
		if ip == nil && *turnPublicIP == "" && *domain != "" {
			// The domain points at this server: its address is the public IP
			ip = lookupIPv4(*domain)
		}
		if ip == nil && *turnPublicIP == "" {
			// No domain either: ask public STUN servers which address we have
			if ip, err = detectPublicIP(); err != nil {
				log.Fatalf("[relay] %v", err)
			}
		}
		if ip == nil {
			log.Fatalf("[relay] -turn-public-ip %q is not an IP address", *turnPublicIP)
		}
		if *turnHost == "" {
			*turnHost = *domain
		}
		min, max, err := parsePortRange(*turnPorts)
		if err != nil {
			log.Fatalf("[relay] %v", err)
		}
		secret, err := loadOrCreateSecret(filepath.Join(*dataDir, "turn-secret"))
		if err != nil {
			log.Fatalf("[relay] TURN secret: %v", err)
		}
		turnSvc, err = startTURN(turnConfig{
			Listen: *turnListen, PublicIP: ip, Host: *turnHost, PortMin: min, PortMax: max,
			Secret: secret, TTL: *turnTTL, MaxAllocations: *turnMaxAllocs,
		})
		if err != nil {
			log.Fatalf("[relay] %v", err)
		}
		slog.Info("[relay] TURN enabled", "listen", *turnListen, "public_ip", ip, "ports", *turnPorts, "urls", turnSvc.urls)
	}

	r := newRelayWith(opts, keys, turnSvc)

	if *apnsKeyPath != "" {
		cli, err := apnsclient.New(apnsclient.Config{
			KeyPath:  *apnsKeyPath,
			KeyID:    *apnsKeyID,
			TeamID:   *apnsTeamID,
			BundleID: *apnsBundleID,
		})
		if err != nil {
			log.Fatalf("[relay] APNs client init failed: %v", err)
		}
		r.apns = cli
		slog.Info("[relay] APNs enabled", "bundle", *apnsBundleID, "key", *apnsKeyID)
	} else {
		slog.Info("[relay] APNs disabled; MsgApnsPush frames will be rejected")
	}

	mux := setupMux(r)

	slog.Info("[relay] starting", "version", version, "require_desktop_key", opts.RequireDesktopKey)
	if *domain == "" {
		slog.Info("[relay] listening", "addr", *addr)
		log.Fatal(newServer(*addr, mux).ListenAndServe())
	}

	// Automatic HTTPS: no reverse proxy needed. Port 80 answers Let's Encrypt's
	// challenge and redirects everything else to https.
	certs := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(*domain),
		Cache:      autocert.DirCache(filepath.Join(*dataDir, "certs")),
	}
	go func() { log.Fatal(newServer(":80", certs.HTTPHandler(nil)).ListenAndServe()) }()
	if addrGiven {
		// Extra plain listener, e.g. for clients that still use ws://host:8080
		go func() { log.Fatal(newServer(*addr, mux).ListenAndServe()) }()
	}
	server := newServer(":443", mux)
	server.TLSConfig = certs.TLSConfig()
	slog.Info("[relay] listening", "https", *domain)
	log.Fatal(server.ListenAndServeTLS("", ""))
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
}

func lookupIPv4(host string) net.IP {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4
		}
	}
	return nil
}

// keepTurnCredentials hands a verified desktop TURN credentials now and again
// at a third of their lifetime, so the ones it passes to phones never expire mid-use.
func (r *relay) keepTurnCredentials(ctx context.Context, me *peer) {
	ticker := time.NewTicker(r.turn.cfg.TTL / 3)
	defer ticker.Stop()
	for {
		r.mu.Lock()
		current := r.desktopCtrl[me.deviceID] == me
		r.mu.Unlock()
		if !current {
			return
		}
		if err := sendSDJSON(ctx, me.conn, sdproto.MsgTurnCredentials, r.turn.credentials(me.deviceID)); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
