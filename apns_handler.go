package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/seedmux/relay/sdproto"
)

// handleApnsPush forwards a MsgApnsPush frame to Apple APNs and ships the
// response back to Desktop as MsgApnsResult. Called in its own goroutine so
// the Apple HTTP/2 roundtrip doesn't block the control connection's read loop.
//
// Relay is stateless — no token persistence, no dedupe. Every push is forwarded
// as-is to Apple, every response is forwarded as-is to Desktop.
func (r *relay) handleApnsPush(ctx context.Context, me *peer, payload []byte) {
	if r.apns == nil {
		slog.Warn("[relay:apns] dropping push, APNs not configured")
		r.respondApnsError(ctx, me, payload, 0, "apns_not_configured")
		return
	}
	if me == nil || me.conn == nil {
		// Desktop disconnected mid-push; nowhere to send the result.
		return
	}

	var req sdproto.ApnsPushPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		slog.Warn("[relay:apns] invalid MsgApnsPush payload", "err", err)
		return
	}

	res := r.apns.Send(req.Token, req.Environment, req.Payload)

	result := sdproto.ApnsResultPayload{
		ID:          req.ID,
		Token:       req.Token,
		Success:     res.Success(),
		HTTPStatus:  res.HTTPStatus,
		AppleReason: res.AppleReason,
	}
	if res.Err != nil && result.AppleReason == "" {
		// Network / client-side error before reaching Apple. Encode it in
		// apple_reason so Desktop logs have something actionable.
		result.AppleReason = res.Err.Error()
	}

	slog.Info("[relay:apns] sent",
		"id", req.ID,
		"token_prefix", safeTokenPrefix(req.Token),
		"env", req.Environment,
		"success", result.Success,
		"http_status", result.HTTPStatus,
		"reason", result.AppleReason)

	if err := sendSDJSON(ctx, me.conn, sdproto.MsgApnsResult, result); err != nil {
		slog.Error("[relay:apns] write MsgApnsResult failed", "id", req.ID, "err", err)
	}
}

// respondApnsError sends a failure MsgApnsResult back to Desktop when the
// push never reached apnsclient (e.g., Relay started without --apns-* flags).
// Desktop logs / swallows; we don't want the push to silently disappear.
func (r *relay) respondApnsError(ctx context.Context, me *peer, payload []byte, httpStatus int, reason string) {
	if me == nil || me.conn == nil {
		return
	}
	var req sdproto.ApnsPushPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return
	}
	result := sdproto.ApnsResultPayload{
		ID:          req.ID,
		Token:       req.Token,
		Success:     false,
		HTTPStatus:  httpStatus,
		AppleReason: reason,
	}
	_ = sendSDJSON(ctx, me.conn, sdproto.MsgApnsResult, result)
}

func safeTokenPrefix(token string) string {
	if len(token) < 8 {
		return token
	}
	return token[:8]
}
