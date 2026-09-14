// Package memory is an in-process wlog.Drain that keeps recent events in a ring
// buffer and lets live code subscribe to new ones — the base wlogtest builds on, and
// useful on its own for a dev "tail my logs" view.
package memory

import (
	"context"
	"maps"
	"sync"
)

const defaultSize = 1000

// subscriberBuffer is how many events a slow subscriber can fall behind before Send
// starts dropping the newest event for that subscriber only (never blocking Send).
const subscriberBuffer = 100

// Memory is a fixed-size ring buffer of events plus a live subscriber fan-out. The
// zero value is not usable; build one with New.
type Memory struct {
	size int

	mu   sync.Mutex
	buf  []map[string]any
	next int // next write index once buf is full

	subMu sync.Mutex
	subs  map[chan map[string]any]struct{}
}

// New returns a Memory holding at most size events (size <= 0 defaults to 1000).
func New(size int) *Memory {
	if size <= 0 {
		size = defaultSize
	}
	return &Memory{size: size, subs: map[chan map[string]any]struct{}{}}
}

// Send implements wlog.Drain: it stores a copy of event in the ring buffer and
// forwards a copy to every live subscriber, non-blocking either way.
func (m *Memory) Send(_ context.Context, event map[string]any) {
	cp := cloneMap(event)

	m.mu.Lock()
	if len(m.buf) < m.size {
		m.buf = append(m.buf, cp)
	} else {
		m.buf[m.next] = cp
		m.next = (m.next + 1) % m.size
	}
	m.mu.Unlock()

	m.fanOut(cp)
}

// Snapshot returns a copy of every buffered event, oldest first.
func (m *Memory) Snapshot() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.buf) < m.size {
		out := make([]map[string]any, len(m.buf))
		for i, e := range m.buf {
			out[i] = cloneMap(e)
		}
		return out
	}
	out := make([]map[string]any, m.size)
	i := 0
	for j := m.next; j < m.size; j++ {
		out[i] = cloneMap(m.buf[j])
		i++
	}
	for j := 0; j < m.next; j++ {
		out[i] = cloneMap(m.buf[j])
		i++
	}
	return out
}

// Subscribe returns a channel of every event Sent from now on. The channel closes
// when ctx is done. A subscriber that falls behind drops the newest event rather than
// blocking Send.
func (m *Memory) Subscribe(ctx context.Context) <-chan map[string]any {
	ch := make(chan map[string]any, subscriberBuffer)

	m.subMu.Lock()
	m.subs[ch] = struct{}{}
	m.subMu.Unlock()

	go func() {
		<-ctx.Done()
		m.subMu.Lock()
		delete(m.subs, ch)
		m.subMu.Unlock()
		close(ch)
	}()

	return ch
}

func (m *Memory) fanOut(event map[string]any) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- event:
		default: // subscriber is behind; drop for it only, never block Send
		}
	}
}

func cloneMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	maps.Copy(cp, m)
	return cp
}
