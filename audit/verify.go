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
// Read top to bottom: each line's hash is recomputed from the exact bytes on disk, with
// the chain and signature values replaced by zeros as the writer did, so a single
// changed byte anywhere fails: JSON whitespace, a reordered key, an integer written as
// a float, a CRLF ending, a duplicate key, or a tampered chain field. The line's
// audit.prev_hash must also match the previous line's hash, which catches a deleted or
// reordered line. A marker line must state the record count and head it really covers,
// so a line cut from the middle fails twice over.
//
// Pass a key to also check audit.signature, the HMAC that Journal wrote over the chain
// hash. With a key, every line must be signed, and a marker's key_id must name that key.
// With no key, signatures are ignored, so an unsigned journal verifies as before.
// Verify only reports; it never repairs a broken chain.
func Verify(path string, keys ...[]byte) error {
	var key []byte
	if len(keys) > 0 {
		key = keys[0]
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		// An empty file carries no chain at all, so it can never be called valid.
		return fmt.Errorf("audit: empty journal")
	}

	prev := ""
	records := 0
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

		if body, ok := event["audit.marker"].(map[string]any); ok {
			if err := checkMarker(body, records, prev, key, n); err != nil {
				return err
			}
		} else {
			records++
		}
		prev = wantHash
	}
	return nil
}

// VerifyHead verifies the whole journal and then checks that its last line ends the
// chain at wantHead.
//
// The head comes from outside the file, such as a monitor that remembers it. That is
// the only check that catches lines cut from the end, because a cut removes the marker
// that would have named them.
func VerifyHead(path, wantHead string, keys ...[]byte) error {
	if err := Verify(path, keys...); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	got, _, err := chainValue(lines[len(lines)-1], "audit.hash")
	if err != nil {
		return fmt.Errorf("audit: head: %w", err)
	}
	if string(got) != wantHead {
		return fmt.Errorf("audit: head %s, want %s", got, wantHead)
	}
	return nil
}

// VerifySigned verifies the journal under key and requires every line to carry a valid
// signature. It is the explicit name for what Verify(path, key) already enforces, for a
// caller that wants to say out loud that an unsigned journal is not good enough.
func VerifySigned(path string, key []byte) error {
	if key == nil {
		return fmt.Errorf("audit: VerifySigned needs a key")
	}
	return Verify(path, key)
}

// checkMarker reports whether a marker line tells the truth about the records before it.
func checkMarker(body map[string]any, records int, prev string, key []byte, n int) error {
	count, ok := body["count"].(float64)
	if !ok || int(count) != records {
		return fmt.Errorf("audit: line %d: marker count %v, want %d", n, body["count"], records)
	}
	if head, _ := body["head"].(string); head != prev {
		return fmt.Errorf("audit: line %d: marker head %q, want %q", n, head, prev)
	}
	if key != nil {
		if id, _ := body["key_id"].(string); id != keyID(key) {
			return fmt.Errorf("audit: line %d: marker key_id %q, want %q", n, body["key_id"], keyID(key))
		}
	}
	return nil
}
