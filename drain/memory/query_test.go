package memory_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/memory"
)

// send stores one event with the given level and timestamp.
func sendEvent(t *testing.T, m *memory.Memory, level string, at time.Time, extra map[string]any) {
	t.Helper()
	event := map[string]any{
		"level":     level,
		"timestamp": at.UTC().Format(time.RFC3339Nano),
	}
	for key, value := range extra {
		event[key] = value
	}
	m.Send(context.Background(), event)
}

// TestMemory_Query proves the filter matches level, time, contained pair, and a custom
// function, and honors Limit.
func TestMemory_Query(t *testing.T) {
	m := memory.New(10)
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	sendEvent(t, m, "info", base, map[string]any{"order_id": "a"})
	sendEvent(t, m, "error", base.Add(time.Minute), map[string]any{"order_id": "b"})
	sendEvent(t, m, "error", base.Add(2*time.Minute), map[string]any{"order_id": "c"})

	if got := m.Query(memory.Filter{Level: "error"}); len(got) != 2 {
		t.Errorf("level filter returned %d events, want 2", len(got))
	}
	from := base.Add(30 * time.Second)
	if got := m.Query(memory.Filter{Since: from}); len(got) != 2 {
		t.Errorf("since filter returned %d events, want 2", len(got))
	}
	if got := m.Query(memory.Filter{Until: from}); len(got) != 1 {
		t.Errorf("until filter returned %d events, want 1", len(got))
	}
	if got := m.Query(memory.Filter{Contains: map[string]any{"order_id": "b"}}); len(got) != 1 {
		t.Errorf("contains filter returned %d events, want 1", len(got))
	}
	custom := m.Query(memory.Filter{Match: func(event map[string]any) bool {
		return event["order_id"] == "c"
	}})
	if len(custom) != 1 {
		t.Errorf("custom filter returned %d events, want 1", len(custom))
	}
	if got := m.Query(memory.Filter{Limit: 2}); len(got) != 2 {
		t.Errorf("limit returned %d events, want 2", len(got))
	}
	if got := m.Query(memory.Filter{Level: "warn"}); len(got) != 0 {
		t.Errorf("no-match filter returned %d events, want 0", len(got))
	}
}

// TestMemory_Clear proves Clear empties the store and keeps it usable.
func TestMemory_Clear(t *testing.T) {
	m := memory.New(2)
	sendEvent(t, m, "info", time.Now(), nil)
	m.Clear()
	if got := m.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot after Clear = %v, want empty", got)
	}
	sendEvent(t, m, "info", time.Now(), nil)
	if got := m.Snapshot(); len(got) != 1 {
		t.Errorf("Snapshot after a new send = %d, want 1", len(got))
	}
}

// TestMemory_QueryHandler proves that GET /events answers a JSON array of the matching
// events, newest first, with a limit.
func TestMemory_QueryHandler(t *testing.T) {
	m := memory.New(10)
	at := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	sendEvent(t, m, "info", at, map[string]any{"event_id": "e-1"})
	sendEvent(t, m, "error", at.Add(time.Second), map[string]any{"event_id": "e-2"})
	sendEvent(t, m, "error", at.Add(2*time.Second), map[string]any{"event_id": "e-3"})

	request := httptest.NewRequest(http.MethodGet, "/events?level=error&limit=1", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	m.QueryHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var events []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &events); err != nil {
		t.Fatalf("the answer is not a JSON array: %v", err)
	}
	if len(events) != 1 || events[0]["event_id"] != "e-3" {
		t.Errorf("events = %v, want the newest error e-3", events)
	}
}

// TestMemory_QueryHandler_Access proves the loopback rule and the token rule.
func TestMemory_QueryHandler_Access(t *testing.T) {
	m := memory.New(10)
	nonLoopback := func() *http.Request {
		request := httptest.NewRequest(http.MethodGet, "/events", nil)
		request.RemoteAddr = "203.0.113.7:1234"
		return request
	}

	response := httptest.NewRecorder()
	m.QueryHandler().ServeHTTP(response, nonLoopback())
	if response.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a non-loopback client", response.Code)
	}

	guarded := m.QueryHandler(memory.WithToken("s3cret"))
	response = httptest.NewRecorder()
	guarded.ServeHTTP(response, nonLoopback())
	if response.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 without the token", response.Code)
	}
	request := nonLoopback()
	request.Header.Set("Authorization", "Bearer s3cret")
	response = httptest.NewRecorder()
	guarded.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 with the token", response.Code)
	}

	response = httptest.NewRecorder()
	m.QueryHandler(memory.WithAnyAddress()).ServeHTTP(response, nonLoopback())
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 with WithAnyAddress", response.Code)
	}
}
