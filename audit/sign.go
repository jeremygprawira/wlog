package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// WithKey signs every journal line, so Verify(path, key) rejects an edit that does not
// carry a matching signature. Without a key the chain still detects an edit, but
// anyone who can write the file can also rebuild a valid chain. With a key the writer
// must hold the key, which raises the bar from "an edit is detectable" to "an edit
// needs the key".
func WithKey(key []byte) Option {
	return func(j *journal) { j.key = key }
}

// keyID returns a short, stable id for a signing key. A marker carries it, so a reader
// learns which key signed the chain without seeing the key.
func keyID(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])[:16]
}

// signature returns the hex HMAC-SHA256 of one hash string.
func signature(key []byte, hash string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(hash))
	return hex.EncodeToString(mac.Sum(nil))
}
