package sdproto

import "encoding/json"

// ApnsPushPayload is the JSON body of a MsgApnsPush (0x08) frame.
//
// Desktop → Relay. Plaintext — Relay needs the token and aps body to deliver
// the notification via Apple's APNs HTTP/2 API on the Desktop's behalf.
type ApnsPushPayload struct {
	// ID is a Desktop-generated opaque token correlating this push with the
	// MsgApnsResult frame Relay will send back. Typically "push-<uuidv7>".
	ID string `json:"id"`
	// Token is the 64-char lowercase hex APNs device token.
	Token string `json:"token"`
	// Environment selects the APNs endpoint:
	//   "sandbox"    → api.sandbox.push.apple.com
	//   "production" → api.push.apple.com
	Environment string `json:"environment"`
	// Payload is the APNs "aps" JSON payload delivered to the device,
	// forwarded to apns2 unchanged. Apple sees this in plaintext.
	Payload json.RawMessage `json:"payload"`
}

// ApnsResultPayload is the JSON body of a MsgApnsResult (0x09) frame.
//
// Relay → Desktop. Plaintext. Echoes the original ID so Desktop can correlate.
// Apple's reason string (on failure) is passed through verbatim so Desktop can
// decide whether to delete the token (e.g., BadDeviceToken, Unregistered) or
// retry later (transient errors).
type ApnsResultPayload struct {
	// ID echoes ApnsPushPayload.ID.
	ID string `json:"id"`
	// Token echoes the original token so Desktop doesn't need an id→token map.
	Token string `json:"token"`
	// Success is true when Apple accepted the notification.
	Success bool `json:"success"`
	// AppleReason is Apple's failure reason (empty on success). Known
	// terminating reasons: "BadDeviceToken", "Unregistered", "DeviceTokenNotForTopic".
	AppleReason string `json:"apple_reason,omitempty"`
	// HTTPStatus is Apple's HTTP response status (200 on success, 4xx/5xx on
	// failure, or 0 if the request never reached Apple — e.g., network error).
	HTTPStatus int `json:"http_status"`
}
