package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// Verify walks path line by line, recomputing each line's audit.hash from its own
// bytes (minus the chain and signature fields, exactly as Chain computed it) and the
// previous line's hash, and reports the first mismatch by line number: an edited byte,
// a deleted line (breaks the next line's prev_hash reference), or two swapped lines all
// surface here. It only ever reports; it never repairs a broken chain.
//
// Pass a key to also check audit.signature, the HMAC over the chain hash that Sign
// added. With no key, the signature is ignored, so an unsigned journal verifies as
// before.
func Verify(path string, keys ...[]byte) error {
	var key []byte
	if len(keys) > 0 {
		key = keys[0]
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	prev := ""
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("audit: line %d: invalid JSON: %w", lineNo, err)
		}
		storedHash, _ := event["audit.hash"].(string)
		storedPrev, _ := event["audit.prev_hash"].(string)
		storedSignature, _ := event["audit.signature"].(string)
		if storedPrev != prev {
			return fmt.Errorf("audit: line %d: prev_hash %q, want %q", lineNo, storedPrev, prev)
		}

		delete(event, "audit.hash")
		delete(event, "audit.prev_hash")
		delete(event, "audit.signature")
		canon, err := canonicalJSON(event)
		if err != nil {
			return fmt.Errorf("audit: line %d: %w", lineNo, err)
		}
		sum := sha256.Sum256(append([]byte(prev), canon...))
		wantHash := hex.EncodeToString(sum[:])
		if storedHash != wantHash {
			return fmt.Errorf("audit: line %d: hash mismatch", lineNo)
		}
		if key != nil {
			if storedSignature == "" {
				return fmt.Errorf("audit: line %d: missing signature", lineNo)
			}
			if signature(key, storedHash) != storedSignature {
				return fmt.Errorf("audit: line %d: signature mismatch", lineNo)
			}
		}
		prev = storedHash
	}
	return scanner.Err()
}
