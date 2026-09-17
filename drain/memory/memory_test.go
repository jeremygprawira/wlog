package memory_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/memory"
)

func send(m *memory.Memory, i int) {
	m.Send(context.Background(), map[string]any{"i": i})
}

func TestMemory_Snapshot_BeforeFull(t *testing.T) {
	m := memory.New(5)
	send(m, 1)
	send(m, 2)

	got := m.Snapshot()
	if len(got) != 2 || got[0]["i"] != 1 || got[1]["i"] != 2 {
		t.Errorf("Snapshot = %v", got)
	}
}

func TestMemory_Snapshot_Wraparound_OldestFirst(t *testing.T) {
	m := memory.New(3)
	for i := 1; i <= 5; i++ {
		send(m, i)
	}

	got := m.Snapshot()
	if len(got) != 3 {
		t.Fatalf("Snapshot has %d entries, want 3", len(got))
	}
	want := []int{3, 4, 5}
	for i, w := range want {
		if got[i]["i"] != w {
			t.Errorf("Snapshot[%d] = %v, want i=%d", i, got[i], w)
		}
	}
}

func TestMemory_Snapshot_IsACopy(t *testing.T) {
	m := memory.New(5)
	send(m, 1)
	got := m.Snapshot()
	got[0]["i"] = 999

	got2 := m.Snapshot()
	if got2[0]["i"] != 1 {
		t.Errorf("mutating a Snapshot result affected the buffer: %v", got2[0])
	}
}

func TestMemory_Subscribe_BothReceive(t *testing.T) {
	m := memory.New(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1 := m.Subscribe(ctx)
	ch2 := m.Subscribe(ctx)

	send(m, 42)

	for _, ch := range []<-chan map[string]any{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev["i"] != 42 {
				t.Errorf("got %v, want i=42", ev)
			}
		case <-time.After(time.Second):
			t.Error("subscriber did not receive the event")
		}
	}
}

func TestMemory_Subscribe_ClosesOnContextDone(t *testing.T) {
	m := memory.New(10)
	ctx, cancel := context.WithCancel(context.Background())
	ch := m.Subscribe(ctx)
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel produced a value instead of closing")
		}
	case <-time.After(time.Second):
		t.Error("channel did not close after ctx was canceled")
	}
}

func TestMemory_Subscribe_SlowSubscriberNeverBlocksSend(t *testing.T) {
	m := memory.New(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Subscribe(ctx) // never read from

	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			send(m, i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Send blocked on a slow subscriber")
	}
}

// TestMemory_SMP1_SubscriberCopy proves a subscriber receives its own deep copy, so a
// subscriber that edits an event cannot change what another subscriber, the ring buffer, or a
// snapshot sees.
func TestMemory_SMP1_SubscriberCopy(t *testing.T) {
	m := memory.New(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := m.Subscribe(ctx)
	second := m.Subscribe(ctx)

	event := map[string]any{
		"user": map[string]any{"id": "u-1", "roles": []any{"reader"}},
	}
	m.Send(context.Background(), event)

	// The first subscriber edits what it received, including a nested slice.
	received := <-first
	received["user"].(map[string]any)["id"] = "tampered"
	received["user"].(map[string]any)["roles"].([]any)[0] = "admin"

	other := <-second
	if other["user"].(map[string]any)["id"] != "u-1" {
		t.Errorf("the second subscriber saw the first one's edit: %v", other)
	}
	if roles := other["user"].(map[string]any)["roles"].([]any); roles[0] != "reader" {
		t.Errorf("a nested slice was shared: %v", roles)
	}

	snapshot := m.Snapshot()
	if len(snapshot) != 1 || snapshot[0]["user"].(map[string]any)["id"] != "u-1" {
		t.Errorf("the snapshot changed through a subscriber: %v", snapshot)
	}

	// The caller's own map is untouched as well.
	if event["user"].(map[string]any)["id"] != "u-1" {
		t.Errorf("a subscriber's edit reached the caller's map: %v", event)
	}
}

// TestMemory_SMP8_NewestN proves Limit keeps the newest matches, not the oldest.
func TestMemory_SMP8_NewestN(t *testing.T) {
	m := memory.New(10)
	for i := 1; i <= 5; i++ {
		m.Send(context.Background(), map[string]any{"level": "error", "n": i})
	}

	got := m.Query(memory.Filter{Level: "error", Limit: 2})
	if len(got) != 2 {
		t.Fatalf("Query returned %d events, want 2", len(got))
	}
	if got[0]["n"] != 4 || got[1]["n"] != 5 {
		t.Errorf("Query returned n=%v, want the newest two (4 and 5)", []any{got[0]["n"], got[1]["n"]})
	}
}

// TestMemory_SMP8_DroppedCounter proves a subscriber that falls behind loses events but not
// the record that it lost them.
func TestMemory_SMP8_DroppedCounter(t *testing.T) {
	m := memory.New(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A subscriber that never reads, and more events than its buffer holds.
	m.Subscribe(ctx)
	for i := 0; i < 500; i++ {
		m.Send(context.Background(), map[string]any{"n": i})
	}
	if m.Dropped() == 0 {
		t.Error("Dropped() = 0 after 500 events for an unread subscriber")
	}
}

// TestMemory_SMP8_ReplayOrder proves the SSE handler subscribes before it reads the replay, so
// an event sent while the replay is on the wire waits in the channel instead of falling
// between the snapshot and the subscription.
func TestMemory_SMP8_ReplayOrder(t *testing.T) {
	m := memory.New(10)
	m.Send(context.Background(), map[string]any{"n": "before"})

	// The writer holds the first write, which is the replay, so the test can send an event
	// at exactly the moment the old code had no subscription yet.
	blocked := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.SSEHandler().ServeHTTP(&blockingWriter{ResponseWriter: w, blocked: blocked, release: release}, r)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get(srv.URL + "?replay=1")
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		// Read the replay and the next live event. The client may hang after that, so the
		// read stops once both arrived.
		buf := make([]byte, 4096)
		total := ""
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) && !strings.Contains(total, "during") {
			n, err := resp.Body.Read(buf)
			total += string(buf[:n])
			if err != nil {
				break
			}
		}
		body <- total
	}()

	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("the handler never wrote the replay")
	}
	m.Send(context.Background(), map[string]any{"n": "during"})
	close(release)

	select {
	case got := <-body:
		if !strings.Contains(got, "during") {
			t.Errorf("an event sent during the replay was lost: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream produced nothing")
	}
	if got := len(m.Snapshot()); got != 2 {
		t.Errorf("the buffer holds %d events, want both", got)
	}
}

// blockingWriter holds the first write until the test releases it, so a test can act while a
// write is in flight.
type blockingWriter struct {
	http.ResponseWriter
	blocked chan struct{}
	release chan struct{}
	once    bool
}

// Write holds the first call, then passes every write through.
func (b *blockingWriter) Write(p []byte) (int, error) {
	if !b.once {
		b.once = true
		close(b.blocked)
		<-b.release
	}
	return b.ResponseWriter.Write(p)
}

// Flush passes a flush through to the underlying writer.
func (b *blockingWriter) Flush() {
	if f, ok := b.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
