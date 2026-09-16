package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/jeremygprawira/wlog"
)

// Sign returns a wlog.Drain that adds audit.signature, an HMAC-SHA256 over the chain
// hash. Register it after Chain and before the journal, so the signed bytes are what the
// journal stores. Verify(path, key) then rejects an edit that does not carry a matching
// signature, which raises the bar from "an edit is detectable" to "an edit needs the
// key".
func Sign(key []byte) wlog.Drain {
	return wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		hash, _ := event["audit.hash"].(string)
		if hash == "" {
			return
		}
		event["audit.signature"] = signature(key, hash)
	})
}

// signature returns the hex HMAC-SHA256 of one hash string.
func signature(key []byte, hash string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(hash))
	return hex.EncodeToString(mac.Sum(nil))
}
