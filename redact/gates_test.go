package redact_test

import (
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

// TestRedact_Apply_ConcurrentSafe is gate G2: many goroutines calling Apply on the same
// *Redactor with their own event maps must not race (run with -race).
func TestRedact_Apply_ConcurrentSafe(t *testing.T) {
	r := redact.Default()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			event := map[string]any{"password": "secret", "id": i, "note": "alice@example.com"}
			r.Apply(event)
			if event["password"] != "[REDACTED]" {
				t.Errorf("concurrent Apply did not mask password")
			}
			if event["note"] != "a***@***.com" {
				t.Errorf("concurrent Apply did not mask email pattern")
			}
		}(i)
	}
	wg.Wait()
}
