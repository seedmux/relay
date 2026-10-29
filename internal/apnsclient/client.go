// Package apnsclient wraps sideshow/apns2 for the Relay. Relay holds one
// shared APNs authentication (a .p8 JWT key) for all Seedmux users; each
// MsgApnsPush frame carries the recipient's device token + environment and is
// forwarded to Apple's APNs HTTP/2 API unchanged.
//
// Relay is stateless with respect to tokens — the Desktop is the canonical
// owner of the apns_tokens table, Relay just pipes.
package apnsclient

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
)

// Config holds the APNs authentication material. All four fields are required
// — Relay refuses to start if any is missing.
type Config struct {
	// KeyPath is the absolute path to the .p8 file from Apple Developer.
	// Must be readable by the relay process (systemd user `nobody` in prod).
	KeyPath string
	// KeyID is the 10-char Key ID Apple assigned when the key was created.
	KeyID string
	// TeamID is the Apple Team ID (10-char), same across all keys for this team.
	TeamID string
	// BundleID is the iOS app's bundle identifier (e.g. com.example.seedmux.ios).
	// Passed as the `apns-topic` header to Apple.
	BundleID string
}

// Client holds two apns2.Client instances — one configured for Apple's
// production host, one for sandbox. Route by ApnsPushPayload.Environment.
type Client struct {
	production *apns2.Client
	sandbox    *apns2.Client
	bundleID   string
}

// New loads the .p8 key, constructs the JWT-based authToken, and returns a
// ready-to-use Client. Fails fast on missing/invalid key material so a broken
// APNs config prevents the relay from starting rather than silently dropping
// pushes.
func New(cfg Config) (*Client, error) {
	if cfg.KeyPath == "" || cfg.KeyID == "" || cfg.TeamID == "" || cfg.BundleID == "" {
		return nil, fmt.Errorf("apnsclient: all of KeyPath/KeyID/TeamID/BundleID are required")
	}
	keyBytes, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("apnsclient: read key file %s: %w", cfg.KeyPath, err)
	}
	authKey, err := token.AuthKeyFromBytes(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("apnsclient: parse .p8 key: %w", err)
	}
	tok := &token.Token{
		AuthKey: authKey,
		KeyID:   cfg.KeyID,
		TeamID:  cfg.TeamID,
	}
	prod := apns2.NewTokenClient(tok).Production()
	sand := apns2.NewTokenClient(tok).Development()
	return &Client{
		production: prod,
		sandbox:    sand,
		bundleID:   cfg.BundleID,
	}, nil
}

// Result captures Apple's response for a single push attempt.
type Result struct {
	HTTPStatus  int
	AppleReason string // Empty on success
	Err         error  // Set when the request never reached Apple (network error, etc.)
}

// Success reports whether Apple accepted the notification (HTTP 200 with no
// error reason).
func (r Result) Success() bool {
	return r.Err == nil && r.HTTPStatus == 200 && r.AppleReason == ""
}

// Send ships one notification to Apple. The `aps` JSON body is forwarded as-is
// (it was already assembled by the Desktop).
//
// environment selects prod vs sandbox:
//
//	"sandbox"    → api.sandbox.push.apple.com (Debug-signed iOS builds)
//	"production" → api.push.apple.com         (Release / App Store / TestFlight)
//
// The two endpoints have independent token spaces — sending a sandbox token to
// production (or vice versa) returns BadDeviceToken, so strict routing here.
func (c *Client) Send(deviceToken, environment string, apsBody json.RawMessage) Result {
	var client *apns2.Client
	switch environment {
	case "sandbox":
		client = c.sandbox
	case "production":
		client = c.production
	default:
		return Result{Err: fmt.Errorf("invalid environment %q (want sandbox|production)", environment)}
	}

	notif := &apns2.Notification{
		DeviceToken: deviceToken,
		Topic:       c.bundleID,
		// apns2 accepts a raw []byte or a Payload builder; we already have the
		// full JSON (including the "aps" wrapper) assembled by Desktop, so feed
		// the raw bytes and let apns2 set Content-Type.
		Payload:    []byte(apsBody),
		Priority:   apns2.PriorityHigh,
		PushType:   apns2.PushTypeAlert,
		Expiration: time.Now().Add(24 * time.Hour),
	}

	resp, err := client.Push(notif)
	if err != nil {
		return Result{Err: err}
	}
	return Result{
		HTTPStatus:  resp.StatusCode,
		AppleReason: resp.Reason,
	}
}

// payload package import kept for callers wanting to use its builder instead
// of raw JSON. Currently unused but preserved to avoid churn if Desktop ever
// switches from raw JSON to structured payload.
var _ = payload.NewPayload
