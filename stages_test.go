package wlog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
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

// TestCore_CORE2_EnricherStructRedacted proves that core copies the value an
// enricher adds, so a struct is walked by its json tags and a map the caller
// still holds never reaches a drain.
func TestCore_CORE2_EnricherStructRedacted(t *testing.T) {
	shared := map[string]any{"password": "hunter2"}

	log, rec := wlogtest.New(t, wlog.WithEnrichers(wlog.EnricherFunc(func(_ context.Context, ev map[string]any) {
		ev["box"] = boxed{Password: "hunter2"}
		ev["shared"] = shared
	})))
	ctx := log.WithContext(context.Background())

	_, end := wlog.Start(ctx, "op")
	end()
	shared["password"] = "changed-after-the-drain"

	line, err := json.Marshal(rec.Last())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(line), "hunter2") {
		t.Errorf("an enricher value leaked a secret: %s", line)
	}
	if strings.Contains(string(line), "changed-after-the-drain") {
		t.Errorf("core stored the map the enricher kept: %s", line)
	}
	if got := rec.Last()["shared"].(map[string]any)["password"]; got == nil {
		t.Errorf("the enricher value vanished: %v", rec.Last()["shared"])
	}
}
