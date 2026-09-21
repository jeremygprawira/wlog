// This file holds the fake sender of the tests, so a pipeline test needs no network.
package wloglambda

import (
	"context"
	"sync"
)

// fakeSender records every event that a pipeline delivers.
type fakeSender struct {
	mu     sync.Mutex
	events []map[string]any
}

// SendBatch records one delivered batch.
func (s *fakeSender) SendBatch(_ context.Context, events []map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, events...)
	return nil
}

// count returns the number of delivered events.
func (s *fakeSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}
