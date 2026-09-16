package wlog_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog"
)

type spyKeeper struct{ keep bool }

func (k spyKeeper) Keep(context.Context, map[string]any) bool { return k.keep }

type spyEnricher struct{ ran *bool }

func (e spyEnricher) Enrich(_ context.Context, event map[string]any) {
	*e.ran = true
	event["password"] = "leaked-if-enrich-runs-after-redact"
}

func TestCore_StageOrder_DroppedEventSkipsEnrichAndSinks(t *testing.T) {
	var enricherRan bool
	log := wlog.New(
		wlog.WithSampler(spyKeeper{keep: false}),
		wlog.WithEnrichers(spyEnricher{ran: &enricherRan}),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "x", 1)
		end()
	})

	if enricherRan {
		t.Error("enricher ran on a dropped event")
	}
	if out != "" {
		t.Errorf("dropped event still wrote output: %q", out)
	}
}

func TestCore_StageOrder_EnricherOutputIsRedacted(t *testing.T) {
	var enricherRan bool
	log := wlog.New(wlog.WithEnrichers(spyEnricher{ran: &enricherRan}))

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	if !enricherRan {
		t.Fatal("enricher did not run")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON line: %v\noutput: %q", err, out)
	}
	if got["password"] != "[REDACTED]" {
		t.Errorf("password (added by enricher) = %v, want [REDACTED] (redact must run after enrich)", got["password"])
	}
}

func TestCore_StageOrder_AuditBypassesSampling(t *testing.T) {
	log := wlog.New(wlog.WithSampler(spyKeeper{keep: false}))

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "audit", map[string]any{"action": "refund"})
		end()
	})

	if out == "" {
		t.Fatal("audit-flagged event was dropped by sampling")
	}
}

func TestCore_StageOrder_KeeperPanicIsIsolated(t *testing.T) {
	panicking := wlog.KeeperFunc(func(context.Context, map[string]any) bool { panic("boom") })
	log := wlog.New(wlog.WithSampler(panicking))

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	// A panicking Keeper falls back to "keep" so a bad sampler never silently
	// swallows every event.
	if out == "" {
		t.Error("panicking Keeper caused the event to be dropped")
	}
}
