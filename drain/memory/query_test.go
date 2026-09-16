package memory_test

import (
	"context"
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

// TestMemory_Named_SameStore proves one name always returns one store, and Stores lists
// it.
func TestMemory_Named_SameStore(t *testing.T) {
	first := memory.Named("test-named", 10)
	second := memory.Named("test-named", 20)
	if first != second {
		t.Fatal("Named returned two stores for one name")
	}
	first.Send(context.Background(), map[string]any{"level": "info"})
	if len(second.Snapshot()) != 1 {
		t.Errorf("the second handle does not see the first handle's event")
	}
	found := false
	for _, name := range memory.Stores() {
		if name == "test-named" {
			found = true
		}
	}
	if !found {
		t.Errorf("Stores() = %v, want test-named", memory.Stores())
	}
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
