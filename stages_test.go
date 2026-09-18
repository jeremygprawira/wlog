package wlog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/memory"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// dropAll is a head sampler that drops every event, so a test can prove what a later
// stage still does with an event the head stage refused.
type dropAll struct{}

// Sample drops every event at rate zero.
func (dropAll) Sample(wlog.Level, string) (bool, float64) { return false, 0 }

type spyEnricher struct{ ran *bool }

func (e spyEnricher) Enrich(_ context.Context, event map[string]any) {
	*e.ran = true
	event["password"] = "leaked-if-enrich-runs-after-redact"
}

func TestCore_StageOrder_DroppedEventSkipsEnrichAndSinks(t *testing.T) {
	var enricherRan bool
	log := wlog.New(
		wlog.WithHeadSampler(dropAll{}),
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
	log := wlog.New(wlog.WithHeadSampler(dropAll{}))

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "audit", map[string]any{"action": "refund"})
		end()
	})

	if out == "" {
		t.Fatal("audit-flagged event was dropped by head sampling")
	}
}

// panickySampler is a head sampler that panics, so a test can prove the head stage
// survives it.
type panickySampler struct{}

// Sample panics.
func (panickySampler) Sample(wlog.Level, string) (bool, float64) { panic("boom") }

func TestCore_StageOrder_KeeperPanicIsIsolated(t *testing.T) {
	panicking := wlog.KeeperFunc(func(context.Context, wlog.Event) bool { panic("boom") })
	log := wlog.New(wlog.WithKeepers(panicking))

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

// TestStages_HeadSamplerPanicIsIsolated proves that a head sampler which panics keeps
// the event, so a broken sampler never silently drops every event.
func TestStages_HeadSamplerPanicIsIsolated(t *testing.T) {
	log := wlog.New(wlog.WithHeadSampler(panickySampler{}))

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	if out == "" {
		t.Error("a panicking head sampler caused the event to be dropped")
	}
}

// TestStages_PAR14_KeeperSeesEnriched proves that enrich runs before the tail keep, so
// a keeper decides on the event a reader would see, not on the bare one.
func TestStages_PAR14_KeeperSeesEnriched(t *testing.T) {
	seen := ""
	log, rec := wlogtest.New(t,
		wlog.WithEnrichers(wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
			event["deploy_region"] = "ap-southeast-1"
		})),
		wlog.WithKeepers(wlog.KeeperFunc(func(_ context.Context, event wlog.Event) bool {
			region, _ := event.Get("deploy_region")
			seen = strings.TrimSpace(region.(string))
			return true
		})),
	)

	_, end := wlog.Start(log.WithContext(context.Background()), "op")
	end()

	if seen != "ap-southeast-1" {
		t.Errorf("the keeper saw %q, want the enriched value", seen)
	}
	if rec.Count() != 1 {
		t.Errorf("events = %d, want 1", rec.Count())
	}
}

// TestStages_HeadDropRescued proves that a head sampler which drops an event does not
// win over a keeper that rescues it, and that the kept event records the rate.
func TestStages_HeadDropRescued(t *testing.T) {
	log, rec := wlogtest.New(t,
		wlog.WithHeadSampler(dropAll{}),
		wlog.WithKeepers(wlog.KeeperFunc(func(_ context.Context, event wlog.Event) bool {
			value, _ := event.Get("keep_me")
			return value == true
		})),
	)
	ctx := log.WithContext(context.Background())

	_, endDropped := wlog.Start(ctx, "dropped")
	endDropped()

	kept, endKept := wlog.Start(ctx, "kept")
	wlog.Set(kept, "keep_me", true)
	endKept()

	if rec.Count() != 1 {
		t.Fatalf("events = %d, want 1 (the head drop alone is not final)", rec.Count())
	}
	if got := counters(rec.Last())["sample_rate"]; got == nil {
		t.Errorf("the rescued event records no sample_rate: %v", rec.Last())
	}
}

// TestStages_SizeCapFinalize proves that finalize holds the size cap and marks the
// event it trimmed. Each value stays under the redactor's string limit, so the cap is
// the thing that trims the event.
func TestStages_SizeCapFinalize(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON), wlog.WithDrains(memory.New(0)))
	chunk := strings.Repeat("x", 32<<10)
	out := captureStdout(t, func() {
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		for i := 0; i < 16; i++ {
			wlog.Set(ctx, fmt.Sprintf("field_%d", i), chunk)
		}
		wlog.Set(ctx, "small", "kept")
		end()
	})

	if len(out) > 256*1024+1024 {
		t.Errorf("the line is %d bytes, want at most the 256 KiB cap", len(out))
	}
	if !strings.Contains(out, `"truncated"`) {
		t.Errorf("the trimmed event carries no wlog.truncated: %s", out[:120])
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
