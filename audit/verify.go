package audit

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// Verify walks path line by line and reports the first line that does not match the
// chain, or nil when every line does.
//
// Read top to bottom: each line's hash is recomputed from the exact bytes on disk,
// with the chain and signature values replaced by zeros as the writer did, so a single
// changed byte anywhere fails: JSON whitespace, a reordered key, an integer written as
// a float, a CRLF ending, a duplicate key, or a tampered chain field. The line's
// audit.prev_hash must also match the previous line's hash, which catches a deleted or
// reordered line. An empty line, and a line without a valid audit.hash, is an error.
//
// Pass a key to also check audit.signature, the HMAC that Journal wrote over the chain
// hash. With no key, the signature is ignored, so an unsigned journal verifies as
// before. Verify only reports; it never repairs a broken chain.
func Verify(path string, keys ...[]byte) error {
	var key []byte
	if len(keys) > 0 {
		key = keys[0]
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	prev := ""
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1] // the newline that ends the last line
	}
	for i, line := range lines {
		n := i + 1
		if len(line) == 0 {
			// The chain covers nothing here, so an empty line is a change to the file
			// that must fail rather than be skipped.
			return fmt.Errorf("audit: line %d: empty line", n)
		}

		storedHash, hashOff, err := chainValue(line, "audit.hash")
		if err != nil {
			return fmt.Errorf("audit: line %d: %w", n, err)
		}
		offsets := []int{hashOff}
		var storedSig []byte
		if sig, sigOff, err := chainValue(line, "audit.signature"); err == nil {
			storedSig = sig
			offsets = append(offsets, sigOff)
		}

		sum := sha256.Sum256(coveredBytes(line, offsets...))
		wantHash := hex.EncodeToString(sum[:])
		if string(storedHash) != wantHash {
			return fmt.Errorf("audit: line %d: hash mismatch", n)
		}

		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("audit: line %d: invalid JSON: %w", n, err)
		}
		if got, _ := event["audit.prev_hash"].(string); got != prev {
			return fmt.Errorf("audit: line %d: prev_hash %q, want %q", n, got, prev)
		}

		if key != nil {
			if storedSig == nil {
				return fmt.Errorf("audit: line %d: missing signature", n)
			}
			if !hmac.Equal([]byte(signature(key, wantHash)), storedSig) {
				return fmt.Errorf("audit: line %d: signature mismatch", n)
			}
		}
		prev = wantHash
	}
	return nil
}
