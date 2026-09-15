package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/jeremygprawira/wlog"
)

// Chain returns a wlog.Drain that hash-chains every event carrying the reserved
// "audit" field: hash = sha256(prev_hash || canonical_json(event)). It sets
// "audit.prev_hash" and "audit.hash" on the event map in place, so register it FIRST
// via wlog.WithDrains — sendToDrains calls drains in order and the console sink runs
// only after all of them return, so any drain (or the console) listed after Chain sees
// the hash too. One Chain() keeps its own chain state (the last hash) behind one mutex,
// so concurrent audit events on the same Logger still form a single, strictly ordered
// chain.
func Chain() wlog.Drain {
	c := &chain{}
	return wlog.DrainFunc(c.send)
}

type chain struct {
	mu   sync.Mutex
	prev string // hex sha256 of the previous link; "" before the first one
}

func (c *chain) send(_ context.Context, event map[string]any) {
	if _, ok := event["audit"]; !ok {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	canon, err := canonicalJSON(event)
	if err != nil {
		// ponytail: a malformed event can't be canonicalized; skip chaining rather than
		// let one bad event break every audit event after it.
		return
	}
	sum := sha256.Sum256(append([]byte(c.prev), canon...))
	hash := hex.EncodeToString(sum[:])

	event["audit.prev_hash"] = c.prev
	event["audit.hash"] = hash
	c.prev = hash
}
