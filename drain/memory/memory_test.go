package memory_test

import (
	"context"
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
