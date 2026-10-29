package sdproto

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	magic   = [2]byte{0x53, 0x44} // "SD"
	version = byte(0x01)
)

const (
	HeaderSize = 12

	MsgAuth             = byte(0x01)
	MsgReconnect        = byte(0x02)
	MsgError            = byte(0x03)
	MsgData             = byte(0x04)
	MsgPaired           = byte(0x05)
	MsgPeerDisconnected = byte(0x06)
	// 0x07 is MsgReconnected (desktop→mobile bridge-ready ACK), defined in
	// the Go module's protocol package but not handled by relay yet.

	// MsgApnsPush: Desktop → Relay control frame asking Relay to deliver an
	// APNs notification on its behalf. Payload is plaintext JSON (see
	// ApnsPushPayload) because Relay must read the token and aps body to
	// forward them to Apple — MsgData encrypted frames would be opaque.
	MsgApnsPush = byte(0x08)
	// MsgApnsResult: Relay → Desktop control frame reporting Apple's response
	// (success, BadDeviceToken, etc.). Payload is plaintext JSON
	// (ApnsResultPayload). Desktop acts on the result (e.g., delete stale
	// token rows from apns_tokens).
	MsgApnsResult = byte(0x09)
	// MsgTurnCredentials: Relay → Desktop control frame with short-lived TURN
	// credentials (TurnCredentialsPayload). Sent to verified desktops right after
	// they register and again before the previous ones expire. Older desktops
	// ignore unknown control frames.
	MsgTurnCredentials = byte(0x0A)
	// MsgTurnRequest: Desktop → Relay, asks for fresh TURN credentials now
	// (empty payload). Answered with MsgTurnCredentials, or an error frame when
	// TURN is off or the desktop is not verified.
	MsgTurnRequest = byte(0x0B)
)

// TurnCredentialsPayload is the JSON body of MsgTurnCredentials, shaped like a
// WebRTC RTCIceServer plus the lifetime in seconds.
type TurnCredentialsPayload struct {
	Type       string   `json:"type"`
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
	TTL        int      `json:"ttl"`
}

type Frame struct {
	MsgType byte
	Flags   byte
	Payload []byte
}

func Pack(msgType, flags byte, payload []byte) []byte {
	frame := make([]byte, HeaderSize+len(payload))
	frame[0] = magic[0]
	frame[1] = magic[1]
	frame[2] = version
	frame[3] = byte(HeaderSize)
	frame[4] = msgType
	frame[5] = flags
	binary.BigEndian.PutUint32(frame[6:10], uint32(len(payload)))
	copy(frame[HeaderSize:], payload)
	return frame
}

func Unpack(data []byte) (*Frame, error) {
	if len(data) < HeaderSize {
		return nil, errors.New("sd: frame too short")
	}
	if data[0] != magic[0] || data[1] != magic[1] {
		return nil, errors.New("sd: invalid magic")
	}
	if data[2] != version {
		return nil, fmt.Errorf("sd: unsupported version 0x%02x", data[2])
	}

	hdrLen := int(data[3])
	if hdrLen < HeaderSize || len(data) < hdrLen {
		return nil, errors.New("sd: invalid header length")
	}

	payloadLen := binary.BigEndian.Uint32(data[6:10])
	totalLen := hdrLen + int(payloadLen)
	if len(data) < totalLen {
		return nil, fmt.Errorf("sd: frame truncated (need %d, got %d)", totalLen, len(data))
	}

	return &Frame{
		MsgType: data[4],
		Flags:   data[5],
		Payload: data[hdrLen:totalLen],
	}, nil
}
