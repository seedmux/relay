package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/seedmux/relay/sdproto"
)

const testKey = "0123456789abcdef0123456789abcdef-desktop-key"

func desktopAuth(deviceID, key string) map[string]string {
	m := map[string]string{"type": "auth", "token": "tok-" + deviceID, "role": "desktop", "deviceId": deviceID}
	if key != "" {
		m["relayKey"] = key
	}
	return m
}

// expectRejected: the relay answers with an error frame and closes the connection
func expectRejected(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	msgType, msg := readJSONFrame(t, conn)
	if msgType != sdproto.MsgError || msg["type"] != "error" {
		t.Fatalf("want error frame, got 0x%02x %v", msgType, msg)
	}
}

func waitDesktop(t *testing.T, r *relay, deviceID string) *peer {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		p := r.desktopCtrl[deviceID]
		r.mu.Unlock()
		if p != nil {
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("desktop %s never registered", deviceID)
	return nil
}

// First key wins; a different key or no key for the same device ID is refused,
// and the original control connection is not replaced
func TestDesktopKeyTrustOnFirstUse(t *testing.T) {
	srv, r := startTestRelayWith(t, newRelay())
	defer srv.Close()
	url := "ws" + srv.URL[4:]

	owner := dial(t, url)
	defer owner.CloseNow()
	sendSD(t, owner, sdproto.MsgAuth, desktopAuth("d1", testKey))
	first := waitDesktop(t, r, "d1")
	if !first.verified {
		t.Fatal("desktop with a key should be verified")
	}

	for _, key := range []string{testKey + "-other", ""} {
		impostor := dial(t, url)
		sendSD(t, impostor, sdproto.MsgAuth, desktopAuth("d1", key))
		expectRejected(t, impostor)
		impostor.CloseNow()
	}
	if waitDesktop(t, r, "d1") != first {
		t.Fatal("an impostor replaced the control connection")
	}

	// The owner reconnecting with its key is accepted
	again := dial(t, url)
	defer again.CloseNow()
	sendSD(t, again, sdproto.MsgAuth, desktopAuth("d1", testKey))
	deadline := time.Now().Add(3 * time.Second)
	for waitDesktop(t, r, "d1") == first && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if waitDesktop(t, r, "d1") == first {
		t.Fatal("the owner should be able to reconnect")
	}
}

// Keys survive restarts
func TestDesktopKeysPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop-keys.json")
	keys, err := loadDesktopKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := keys.check("d1", testKey); !ok || err != nil {
		t.Fatalf("first use: %v %v", ok, err)
	}
	reloaded, err := loadDesktopKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.check("d1", "x"+testKey); err != errDesktopKeyMismatch {
		t.Fatalf("after restart a different key must be refused, got %v", err)
	}
	if _, err := reloaded.check("d2", "short"); err != errDesktopKeyInvalid {
		t.Fatalf("short keys must be refused, got %v", err)
	}
}

// -require-desktop-key: desktops without a key cannot connect at all
func TestRequireDesktopKey(t *testing.T) {
	opts := defaultOptions()
	opts.RequireDesktopKey = true
	keys, _ := loadDesktopKeys("")
	srv, _ := startTestRelayWith(t, newRelayWith(opts, keys, nil))
	defer srv.Close()
	conn := dial(t, "ws"+srv.URL[4:])
	defer conn.CloseNow()
	sendSD(t, conn, sdproto.MsgAuth, desktopAuth("legacy", ""))
	expectRejected(t, conn)
}

// Phones and unauthenticated sockets cannot use the shared APNs key
func TestApnsPushDesktopsOnly(t *testing.T) {
	srv, _ := startTestRelayWith(t, newRelay())
	defer srv.Close()
	conn := dial(t, "ws"+srv.URL[4:])
	defer conn.CloseNow()
	sendSD(t, conn, sdproto.MsgApnsPush, map[string]string{"token": "abc", "environment": "production"})
	msgType, msg := readJSONFrame(t, conn)
	if msgType != sdproto.MsgError || !strings.Contains(msg["message"].(string), "desktops only") {
		t.Fatalf("want desktops-only error, got 0x%02x %v", msgType, msg)
	}
}

// A socket that never authenticates is closed after the auth timeout
func TestAuthTimeout(t *testing.T) {
	opts := defaultOptions()
	opts.AuthTimeout = 200 * time.Millisecond
	keys, _ := loadDesktopKeys("")
	srv, _ := startTestRelayWith(t, newRelayWith(opts, keys, nil))
	defer srv.Close()
	conn := dial(t, "ws"+srv.URL[4:])
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("relay should have closed the idle socket (err=%v, ctx=%v)", err, ctx.Err())
	}
}

// Concurrent connections per IP are capped
func TestConnectionsPerIPCapped(t *testing.T) {
	opts := defaultOptions()
	opts.MaxConnsPerIP = 2
	keys, _ := loadDesktopKeys("")
	srv, _ := startTestRelayWith(t, newRelayWith(opts, keys, nil))
	defer srv.Close()
	url := "ws" + srv.URL[4:]
	a, b := dial(t, url), dial(t, url)
	defer a.CloseNow()
	defer b.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, url, nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third connection should get 429, got err=%v resp=%v", err, resp)
	}
}

// Verified desktops get TURN credentials right after registering; legacy ones do not
func TestTurnCredentialsForVerifiedDesktops(t *testing.T) {
	turnSvc, err := startTURN(turnConfig{
		Listen: "127.0.0.1:0", PublicIP: net.ParseIP("127.0.0.1"), PortMin: 51000, PortMax: 51010,
		Secret: strings.Repeat("s", 40), TTL: time.Hour, MaxAllocations: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer turnSvc.Close()
	keys, _ := loadDesktopKeys("")
	srv, _ := startTestRelayWith(t, newRelayWith(defaultOptions(), keys, turnSvc))
	defer srv.Close()
	url := "ws" + srv.URL[4:]

	desktop := dial(t, url)
	defer desktop.CloseNow()
	sendSD(t, desktop, sdproto.MsgAuth, desktopAuth("d1", testKey))
	frame := readFrame(t, desktop)
	if frame.MsgType != sdproto.MsgTurnCredentials {
		t.Fatalf("want TURN credentials, got 0x%02x", frame.MsgType)
	}
	var creds sdproto.TurnCredentialsPayload
	if err := json.Unmarshal(frame.Payload, &creds); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(creds.Username, ":d1") || creds.Credential == "" || len(creds.URLs) != 2 || creds.TTL != 3600 {
		t.Fatalf("bad credentials: %+v", creds)
	}
	// The relay's own TURN accepts them, bound to the desktop
	userID, key, ok := turnSvc.authenticate(&turnAttrs{Username: creds.Username, Realm: "seedmux"})
	if !ok || userID != "d1" || len(key) == 0 {
		t.Fatalf("relay should accept its own credentials: %v %q", ok, userID)
	}

	// Legacy desktop (no key): no credentials, and asking is refused
	legacy := dial(t, url)
	defer legacy.CloseNow()
	sendSD(t, legacy, sdproto.MsgAuth, desktopAuth("old", ""))
	sendSD(t, legacy, sdproto.MsgTurnRequest, map[string]string{})
	msgType, _ := readJSONFrame(t, legacy)
	if msgType != sdproto.MsgError {
		t.Fatalf("legacy desktop should not get TURN, got 0x%02x", msgType)
	}
}

// Expired or forged credentials are refused
func TestTurnRejectsBadCredentials(t *testing.T) {
	svc := &turnService{cfg: turnConfig{Secret: strings.Repeat("s", 40), TTL: time.Hour}, allocs: map[string]int{}}
	expired := "1:d1"
	if _, _, ok := svc.authenticate(&turnAttrs{Username: expired, Realm: "seedmux"}); ok {
		t.Fatal("expired credentials must be refused")
	}
	if _, _, ok := svc.authenticate(&turnAttrs{Username: "not-a-username", Realm: "seedmux"}); ok {
		t.Fatal("malformed usernames must be refused")
	}
}

// TURN must not relay into private networks (SSRF), but may reach public peers and itself
func TestTurnPermissionsBlockPrivatePeers(t *testing.T) {
	svc := &turnService{cfg: turnConfig{PublicIP: net.ParseIP("203.0.113.7")}}
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "192.168.31.53", "172.16.0.1", "169.254.169.254", "100.64.1.1", "0.0.0.0", "224.0.0.1", "::1", "fe80::1", "fd00::1"} {
		if svc.permitted(nil, net.ParseIP(ip)) {
			t.Errorf("must refuse relaying to %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "203.0.113.7", "2001:4860:4860::8888"} {
		if !svc.permitted(nil, net.ParseIP(ip)) {
			t.Errorf("should allow relaying to %s", ip)
		}
	}
}

// Quota: a desktop cannot hold more than MaxAllocations at once
func TestTurnQuota(t *testing.T) {
	svc := &turnService{cfg: turnConfig{MaxAllocations: 2}, allocs: map[string]int{}}
	user := "9999999999:d1"
	svc.countAllocation("d1", 1)
	if !svc.underQuota(user, "", nil) {
		t.Fatal("one allocation is under quota")
	}
	svc.countAllocation("d1", 1)
	if svc.underQuota(user, "", nil) {
		t.Fatal("two allocations reach the quota of 2")
	}
	svc.countAllocation("d1", -1)
	if !svc.underQuota(user, "", nil) {
		t.Fatal("after one is released the desktop is under quota again")
	}
}
