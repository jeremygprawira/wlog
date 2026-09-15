package main

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// TestCustomDrain_ReceivesEvent proves a hand-written Drain is called once per event.
func TestCustomDrain_ReceivesEvent(t *testing.T) {
	drain := &countingDrain{}
	log := wlog.New(wlog.WithDrains(drain))

	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "job.run")
	end()

	if got := drain.Count(); got != 1 {
		t.Errorf("Count() = %d, want 1", got)
	}
}
