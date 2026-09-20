// This file checks the exported Matches filter that the drain/file readers use, and the
// query endpoint's rejection of a bad time parameter.
package memory_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/memory"
)

// TestMemory_Matches proves that Matches covers the level, the time window, a contained
// pair, and the custom function, and that an event without a usable timestamp fails a
// time filter.
func TestMemory_Matches(t *testing.T) {
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	event := map[string]any{
		"level":     "error",
		"timestamp": at.Format(time.RFC3339Nano),
		"status":    float64(500),
	}
	cases := []struct {
		name   string
		filter memory.Filter
		want   bool
	}{
		{"empty filter", memory.Filter{}, true},
		{"level match", memory.Filter{Level: "error"}, true},
		{"level miss", memory.Filter{Level: "info"}, false},
		{"since before", memory.Filter{Since: at.Add(-time.Minute)}, true},
		{"since after", memory.Filter{Since: at.Add(time.Minute)}, false},
		{"until after", memory.Filter{Until: at.Add(time.Minute)}, true},
		{"until before", memory.Filter{Until: at.Add(-time.Minute)}, false},
		{"contains match", memory.Filter{Contains: map[string]any{"status": float64(500)}}, true},
		{"contains miss", memory.Filter{Contains: map[string]any{"status": float64(200)}}, false},
		{"custom match", memory.Filter{Match: func(map[string]any) bool { return true }}, true},
		{"custom miss", memory.Filter{Match: func(map[string]any) bool { return false }}, false},
	}
	for _, c := range cases {
		if got := memory.Matches(event, c.filter); got != c.want {
			t.Errorf("%s: Matches = %v, want %v", c.name, got, c.want)
		}
	}
	if memory.Matches(map[string]any{"level": "error"}, memory.Filter{Since: at}) {
		t.Error("an event without a timestamp passed a since filter")
	}
	if memory.Matches(map[string]any{"timestamp": 42}, memory.Filter{Since: at}) {
		t.Error("an event with a non-string timestamp passed a since filter")
	}
}

// TestMemory_QueryHandlerBadTime proves that the query endpoint rejects a bad time
// parameter with a client error.
func TestMemory_QueryHandlerBadTime(t *testing.T) {
	handler := memory.New(1).QueryHandler()
	targets := []string{
		"/events?since=nope",
		"/events?until=13monkeys",
		"/events?since=2026-09-21T12:00:00Z&until=nope",
	}
	for _, target := range targets {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		// The default guard allows a loopback caller only.
		request.RemoteAddr = "127.0.0.1:12345"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, recorder.Code)
		}
	}
}
