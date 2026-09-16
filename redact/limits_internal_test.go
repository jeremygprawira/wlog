package redact

import (
	"strconv"
	"testing"
)

// cacheSize reports how many keys the token cache holds, for the bound test.
func (r *Redactor) cacheSize() int {
	n := 0
	r.tokenCache.Range(func(any, any) bool { n++; return true })
	return n
}

// TestRedact_RED4_CacheBounded proves that the token cache never grows past its
// bound, so a caller who sends a new key on every event cannot grow the process.
func TestRedact_RED4_CacheBounded(t *testing.T) {
	r := MustNew()
	for i := 0; i < 3*cacheLimit; i++ {
		key := "key_" + strconv.Itoa(i)
		r.cachedTokenize(key)
	}
	if got := r.cacheSize(); got > cacheLimit {
		t.Errorf("cache holds %d keys, want at most %d", got, cacheLimit)
	}
}
