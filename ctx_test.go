package wlog_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

type ctxMarkerKey struct{}

// TestEnricher_SeesEventContext proves an Enricher runs with the context the event was
// started from, so an enricher that reads request-scoped values (a trace span, a
// tenant id) can find them. Without this, trace-otel cannot see the active span.
func TestEnricher_SeesEventContext(t *testing.T) {
	var seen any
	enricher := wlog.EnricherFunc(func(ctx context.Context, _ map[string]any) {
		seen = ctx.Value(ctxMarkerKey{})
	})
	log, _ := wlogtest.New(t, wlog.WithEnrichers(enricher))

	ctx := log.WithContext(context.WithValue(context.Background(), ctxMarkerKey{}, "marker"))
	_, end := wlog.Start(ctx, "op")
	end()

	if seen != "marker" {
		t.Errorf("enricher ctx value = %v, want marker", seen)
	}
}

// TestDrain_CtxNotCanceledByEventEnd proves ending an event sends to drains with a
// context that is still usable, even when the event's own context is canceled first.
// A drain makes network calls, so a canceled request context must not abort them.
func TestDrain_CtxNotCanceledByEventEnd(t *testing.T) {
	var gotErr error
	drain := wlog.DrainFunc(func(ctx context.Context, _ map[string]any) { gotErr = ctx.Err() })
	log := wlog.New(wlog.WithDrains(drain))

	reqCtx, cancel := context.WithCancel(context.Background())
	ctx := log.WithContext(reqCtx)
	_, end := wlog.Start(ctx, "op")
	cancel()
	captureStdout(t, func() {
		end()
		flushWriter(t, log)
	})

	if gotErr != nil {
		t.Errorf("drain ctx error = %v, want nil after the request context canceled", gotErr)
	}
}
