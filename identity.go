package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Desktop identity: trust on first use.
//
// Anyone can open a WebSocket and claim to be desktop X. Without a check, an
// impostor replaces X's control connection: X's phones are then routed to the
// impostor when they reconnect (end-to-end encryption keeps the content
// unreadable, but X becomes unreachable), and the impostor can use X's APNs
// push and TURN quota.
//
// A desktop therefore sends a random relay key (at least 32 characters) with
// its auth and bind frames. The first key seen for a device ID is remembered
// (as a SHA-256 hash); later connections for that ID must present the same key.
// Desktops that never sent a key (older daemons) are accepted unless the relay
// runs with -require-desktop-key, but they are not "verified" and get no TURN
// credentials.

var (
	errDesktopKeyMismatch = errors.New("desktop identity mismatch: this device ID is registered with a different relay key")
	errDesktopKeyRequired = errors.New("desktop identity required: this device ID is registered with a relay key")
	errDesktopKeyInvalid  = errors.New("relay key too short (need at least 32 characters)")
)

const minDesktopKeyLen = 32

type desktopKeys struct {
	mu     sync.Mutex
	path   string // empty = keep in memory only (tests)
	hashes map[string]desktopKeyRecord
}

type desktopKeyRecord struct {
	KeyHash string `json:"keyHash"`
	Since   int64  `json:"since"`
}

type desktopKeyFile struct {
	Version  int                         `json:"version"`
	Desktops map[string]desktopKeyRecord `json:"desktops"`
}

func loadDesktopKeys(path string) (*desktopKeys, error) {
	k := &desktopKeys{path: path, hashes: map[string]desktopKeyRecord{}}
	if path == "" {
		return k, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return k, nil
	}
	if err != nil {
		return nil, err
	}
	var file desktopKeyFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Desktops != nil {
		k.hashes = file.Desktops
	}
	return k, nil
}

// check verifies (or, on first use, registers) the key for a desktop.
// verified is true when the desktop proved possession of its registered key.
func (k *desktopKeys) check(deviceID, key string) (verified bool, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	record, known := k.hashes[deviceID]
	if key == "" {
		if known {
			return false, errDesktopKeyRequired
		}
		return false, nil
	}
	if len(key) < minDesktopKeyLen {
		return false, errDesktopKeyInvalid
	}
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	if known {
		if subtle.ConstantTimeCompare([]byte(record.KeyHash), []byte(hash)) != 1 {
			return false, errDesktopKeyMismatch
		}
		return true, nil
	}
	k.hashes[deviceID] = desktopKeyRecord{KeyHash: hash, Since: time.Now().Unix()}
	if err := k.saveLocked(); err != nil {
		delete(k.hashes, deviceID)
		return false, err
	}
	return true, nil
}

func (k *desktopKeys) saveLocked() error {
	if k.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(desktopKeyFile{Version: 1, Desktops: k.hashes}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return err
	}
	tmp := k.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, k.path)
}
