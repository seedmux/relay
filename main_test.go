package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/seedmux/relay/sdproto"
)

func startTestRelay(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := startTestRelayWith(t, newRelay())
	return srv
}

func startTestRelayWith(t *testing.T, r *relay) (*httptest.Server, *relay) {
	t.Helper()
	return httptest.NewServer(setupMux(r)), r
}

// waitWaiting waits until a desktop is queued on the pairing token. A phone that
// arrives before its desktop is rejected right away (no QR without a desktop),
// so tests must not race the two auth frames.
func waitWaiting(t *testing.T, r *relay, token string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.waiting[token])
		r.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("desktop never queued on token %q", token)
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func sendSD(t *testing.T, conn *websocket.Conn, msgType byte, payload interface{}) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, sdproto.Pack(msgType, 0, data)); err != nil {
		t.Fatal(err)
	}
}

func readRaw(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readFrame(t *testing.T, conn *websocket.Conn) *sdproto.Frame {
	t.Helper()
	frame, err := sdproto.Unpack(readRaw(t, conn))
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func readJSONFrame(t *testing.T, conn *websocket.Conn) (byte, map[string]interface{}) {
	t.Helper()
	frame := readFrame(t, conn)

	var payload map[string]interface{}
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	return frame.MsgType, payload
}

func TestPairingFlow(t *testing.T) {
	srv, r := startTestRelayWith(t, newRelay())
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]

	desktop := dial(t, wsURL)
	defer desktop.CloseNow()

	mobile := dial(t, wsURL)
	defer mobile.CloseNow()

	sendSD(t, desktop, sdproto.MsgAuth, map[string]string{
		"type":      "auth",
		"token":     "test-token",
		"role":      "desktop",
		"deviceId":  "desktop-001",
		"publicKey": "desktop-pubkey-base64",
	})

	waitWaiting(t, r, "test-token")
	sendSD(t, mobile, sdproto.MsgAuth, map[string]string{
		"type":      "auth",
		"token":     "test-token",
		"role":      "mobile",
		"deviceId":  "mobile-001",
		"publicKey": "mobile-pubkey-base64",
		"proof":     "mobile-proof-hex",
	})

	desktopMsgType, desktopMsg := readJSONFrame(t, desktop)
	if desktopMsgType != sdproto.MsgPaired {
		t.Fatalf("desktop expected MsgPaired, got 0x%02x", desktopMsgType)
	}
	if desktopMsg["type"] != "paired" {
		t.Fatalf("desktop expected paired, got %v", desktopMsg["type"])
	}
	if desktopMsg["peerDeviceId"] != "mobile-001" {
		t.Fatalf("desktop peerDeviceId: got %v", desktopMsg["peerDeviceId"])
	}
	if desktopMsg["peerPublicKey"] != "mobile-pubkey-base64" {
		t.Fatalf("desktop peerPublicKey: got %v", desktopMsg["peerPublicKey"])
	}
	if desktopMsg["peerProof"] != "mobile-proof-hex" {
		t.Fatalf("desktop peerProof: got %v", desktopMsg["peerProof"])
	}

	mobileMsgType, mobileMsg := readJSONFrame(t, mobile)
	if mobileMsgType != sdproto.MsgPaired {
		t.Fatalf("mobile expected MsgPaired, got 0x%02x", mobileMsgType)
	}
	if mobileMsg["type"] != "paired" {
		t.Fatalf("mobile expected paired, got %v", mobileMsg["type"])
	}

	ctx := context.Background()
	payload := sdproto.Pack(sdproto.MsgData, 0, []byte("encrypted-data-from-desktop"))
	if err := desktop.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatal(err)
	}
	if got := readRaw(t, mobile); string(got) != string(payload) {
		t.Fatal("binary forward desktop→mobile failed")
	}

	payload2 := sdproto.Pack(sdproto.MsgData, 0, []byte("encrypted-data-from-mobile"))
	if err := mobile.Write(ctx, websocket.MessageBinary, payload2); err != nil {
		t.Fatal(err)
	}
	if got := readRaw(t, desktop); string(got) != string(payload2) {
		t.Fatal("binary forward mobile→desktop failed")
	}
}

func TestPeerDisconnect(t *testing.T) {
	srv, r := startTestRelayWith(t, newRelay())
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]

	desktop := dial(t, wsURL)
	defer desktop.CloseNow()

	mobile := dial(t, wsURL)

	sendSD(t, desktop, sdproto.MsgAuth, map[string]string{"type": "auth", "token": "t1", "role": "desktop", "deviceId": "d1"})
	waitWaiting(t, r, "t1")
	sendSD(t, mobile, sdproto.MsgAuth, map[string]string{"type": "auth", "token": "t1", "role": "mobile", "deviceId": "m1"})
	readJSONFrame(t, desktop)
	readJSONFrame(t, mobile)

	mobile.Close(websocket.StatusNormalClosure, "bye")

	msgType, msg := readJSONFrame(t, desktop)
	if msgType != sdproto.MsgPeerDisconnected {
		t.Fatalf("expected MsgPeerDisconnected, got 0x%02x", msgType)
	}
	if msg["type"] != "peer_disconnected" {
		t.Fatalf("expected peer_disconnected, got %v", msg["type"])
	}
}

func TestReconnectForward(t *testing.T) {
	srv := startTestRelay(t)
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]

	desktop := dial(t, wsURL)
	defer desktop.CloseNow()

	sendSD(t, desktop, sdproto.MsgAuth, map[string]string{"type": "auth", "token": "t2", "role": "desktop", "deviceId": "d2"})

	mobile := dial(t, wsURL)
	defer mobile.CloseNow()

	sendSD(t, mobile, sdproto.MsgReconnect, map[string]interface{}{
		"type":         "reconnect",
		"deviceId":     "m2",
		"peerDeviceId": "d2",
		"salt":         "aabbccdd",
		"ts":           1710582000,
		"authTag":      "some-auth-tag",
	})

	msgType, msg := readJSONFrame(t, desktop)
	if msgType != sdproto.MsgReconnect {
		t.Fatalf("expected MsgReconnect forwarded, got 0x%02x", msgType)
	}
	if msg["type"] != "reconnect" {
		t.Fatalf("expected reconnect forwarded, got %v", msg["type"])
	}
	if msg["deviceId"] != "m2" {
		t.Fatalf("expected deviceId m2, got %v", msg["deviceId"])
	}
	if msg["salt"] != "aabbccdd" {
		t.Fatalf("salt not forwarded verbatim: got %v", msg["salt"])
	}
}

func TestReconnectPeerOffline(t *testing.T) {
	srv := startTestRelay(t)
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]

	mobile := dial(t, wsURL)
	defer mobile.CloseNow()

	sendSD(t, mobile, sdproto.MsgReconnect, map[string]interface{}{
		"type":         "reconnect",
		"deviceId":     "m3",
		"peerDeviceId": "d3",
		"salt":         "1234",
		"ts":           1710582000,
		"authTag":      "tag",
	})

	msgType, msg := readJSONFrame(t, mobile)
	if msgType != sdproto.MsgError {
		t.Fatalf("expected MsgError waiting, got 0x%02x", msgType)
	}
	if msg["type"] != "waiting" {
		t.Fatalf("expected waiting, got %v", msg["type"])
	}
}
