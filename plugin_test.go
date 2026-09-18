package wlog_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/redact"
)

// multiPlugin implements every optional plugin hook, to prove one struct can serve as
// setup, starter, enricher, keeper, measurer, finisher, and drain at once.
type multiPlugin struct {
	setupCalled bool
	startKind   string
	measures    []wlog.Measure
	finishEvent wlog.Event
	sent        []map[string]any
}

func (p *multiPlugin) Name() string { return "multi" }
func (p *multiPlugin) OnStart(_ context.Context, kind string) context.Context {
	p.startKind = kind
	return context.Background()
}
func (p *multiPlugin) Setup(l *wlog.Logger) error {
	p.setupCalled = true
	return nil
}
func (p *multiPlugin) Enrich(_ context.Context, event map[string]any) { event["tenant"] = "acme" }
func (p *multiPlugin) Keep(context.Context, wlog.Event) bool          { return true }
func (p *multiPlugin) Measure(_ context.Context, m wlog.Measure)      { p.measures = append(p.measures, m) }
func (p *multiPlugin) OnFinish(_ context.Context, event wlog.Event)   { p.finishEvent = event }
func (p *multiPlugin) Send(_ context.Context, event map[string]any) {
	p.sent = append(p.sent, event)
}

func TestCore_Plugin_AllHooksWired(t *testing.T) {
	p := &multiPlugin{}
	log := wlog.New(wlog.WithPlugins(p))

	if !p.setupCalled {
		t.Fatal("Setup was not called")
	}

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON line: %v\noutput: %q", err, out)
	}
	if got["tenant"] != "acme" {
		t.Errorf("plugin Enrich did not run: %v", got)
	}
	if len(p.sent) != 1 {
		t.Errorf("plugin Send (as a drain) received %d events, want 1", len(p.sent))
	}
	if p.startKind != "work" {
		t.Errorf("OnStart kind = %q, want work", p.startKind)
	}
	if len(p.measures) != 1 || p.measures[0].Operation != "op" {
		t.Errorf("measures = %v, want one for the operation", p.measures)
	}
	if p.finishEvent == nil {
		t.Fatal("OnFinish did not run")
	}
	if summary, _ := p.finishEvent.Get("summary"); summary == nil {
		t.Error("the Finisher saw no summary, so it ran before finalize")
	}
}

// TestHooks_PAR25_FinisherSeesEvent proves that a Finisher reads the finished event:
// the summary and the outcome that finalize built, and the fields the redactor left.
func TestHooks_PAR25_FinisherSeesEvent(t *testing.T) {
	seen := make(chan wlog.Event, 1)
	log := wlog.New(
		wlog.WithPlugins(finisherPlugin{seen: seen}),
		wlog.WithDrains(wlog.DrainFunc(func(_ context.Context, event map[string]any) {
			// A drain runs after the Finisher, so the Finisher has already seen
			// this same event.
			if event["password"] != "[REDACTED]" {
				t.Errorf("the drain saw password = %v, want it redacted", event["password"])
			}
		})),
	)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Set(ctx, "password", "hunter2")
	end()

	select {
	case event := <-seen:
		summary, _ := event.Get("summary")
		if summary == nil || summary == "" {
			t.Error("the Finisher saw no summary, so it ran before finalize")
		}
		outcome, _ := event.Get("outcome")
		if outcome != "success" {
			t.Errorf("outcome = %v, want success", outcome)
		}
		password, _ := event.Get("password")
		if password != "[REDACTED]" {
			t.Errorf("password = %v, want the redacted value", password)
		}
		if event.Kind() != "work" {
			t.Errorf("kind = %q, want work", event.Kind())
		}
	default:
		t.Fatal("the Finisher did not run")
	}
}

// finisherPlugin records the event its OnFinish receives.
type finisherPlugin struct{ seen chan wlog.Event }

// Name names the plugin.
func (finisherPlugin) Name() string { return "finisher" }

// OnFinish sends the finished event to the test.
func (p finisherPlugin) OnFinish(_ context.Context, event wlog.Event) { p.seen <- event }

// measurePlugin records every Measure its hook receives.
type measurePlugin struct{ measures []wlog.Measure }

// Name names the plugin.
func (*measurePlugin) Name() string { return "measure" }

// Measure records one measurement.
func (p *measurePlugin) Measure(_ context.Context, m wlog.Measure) {
	p.measures = append(p.measures, m)
}

// TestHooks_MeasurerBeforeSampling proves that a Measurer reads every non-log event,
// even one that head sampling and the level filter both drop afterwards.
func TestHooks_MeasurerBeforeSampling(t *testing.T) {
	p := &measurePlugin{}
	log := wlog.New(
		wlog.WithPlugins(p),
		wlog.WithHeadSampler(dropAll{}),
		wlog.WithLevel(wlog.LevelError),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.SetGroup(ctx, "http", "status", 503, "method", "GET", "route", "/orders")
		wlog.SetLevel(ctx, wlog.LevelInfo)
		end()
		wlog.Info(log.WithContext(context.Background()), "a plain line")
	})

	if out != "" {
		t.Errorf("the event reached a writer, so neither drop ran: %q", out)
	}
	if len(p.measures) != 1 {
		t.Fatalf("measures = %d, want 1 (a log line is not measured)", len(p.measures))
	}
	m := p.measures[0]
	if m.Kind != "work" || m.Operation != "op" || m.Level != wlog.LevelInfo || m.Outcome != "success" {
		t.Errorf("measure = %+v, want the kind, operation, level, and outcome of the event", m)
	}
	if m.Status != "503" || m.Method != http.MethodGet || m.Route != "/orders" {
		t.Errorf("measure = %+v, want the http fields of the event", m)
	}
}

// TestHooks_MeasurerValuePatterns proves that a value pattern applies to a Measure, so
// a metric never carries a value a log line would hide.
func TestHooks_MeasurerValuePatterns(t *testing.T) {
	p := &measurePlugin{}
	log := wlog.New(
		wlog.WithPlugins(p),
		wlog.WithRedactor(redact.MustNew(redact.AddPatterns(redact.Pattern{
			Name: "order-ref", Regex: `ORD-[0-9]+`, Replacement: "[REDACTED]",
		}))),
		wlog.WithHeadSampler(dropAll{}),
	)

	captureStdout(t, func() {
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		wlog.SetGroup(ctx, "http", "route", "/orders/ORD-42")
		end()
	})

	if len(p.measures) != 1 {
		t.Fatalf("measures = %d, want 1", len(p.measures))
	}
	if route := p.measures[0].Route; !strings.Contains(route, "[REDACTED]") {
		t.Errorf("route = %q, want the value pattern applied", route)
	}
}

// setupDrain is a drain that also has a Setup hook, so a test proves New calls it.
type setupDrain struct {
	calls int
	err   error
}

// Setup records the call and returns the configured error.
func (d *setupDrain) Setup(*wlog.Logger) error { d.calls++; return d.err }

// Send records nothing, because only Setup is under test.
func (d *setupDrain) Send(context.Context, map[string]any) {}

// TestHooks_SetupOnDrain proves that New calls Setup on a drain, and that a drain whose
// Setup fails is reported with the drain's name.
func TestHooks_SetupOnDrain(t *testing.T) {
	d := &setupDrain{}
	wlog.New(wlog.WithDrains(d))
	if d.calls != 1 {
		t.Errorf("Setup ran %d times, want once", d.calls)
	}

	problems := make(chan wlog.Problem, 1)
	failing := &setupDrain{err: errors.New("no endpoint")}
	wlog.New(
		wlog.WithDrains(failing),
		wlog.OnProblem(func(p wlog.Problem) { problems <- p }),
	)
	select {
	case p := <-problems:
		if p.Code != "WLOG_INVALID_CONFIG" {
			t.Errorf("code = %q, want WLOG_INVALID_CONFIG", p.Code)
		}
		if !strings.Contains(p.Source, "setupDrain") {
			t.Errorf("source = %q, want the drain's name", p.Source)
		}
	default:
		t.Error("a failing drain Setup reported nothing")
	}
}

type panicPlugin struct{}

func (panicPlugin) Name() string                           { return "panicky" }
func (panicPlugin) Enrich(context.Context, map[string]any) { panic("boom") }

func TestCore_Plugin_PanicIsolatedAndReported(t *testing.T) {
	var errs []string
	log := wlog.New(
		wlog.WithPlugins(panicPlugin{}),
		wlog.OnProblem(func(p wlog.Problem) { errs = append(errs, fmt.Sprintf("%s: %v", p.Source, p.Err)) }),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})
	if out == "" {
		t.Fatal("event was lost after a plugin panic")
	}
	if len(errs) != 1 || errs[0] != "panicky: panic: boom" {
		t.Errorf("errs = %v, want one entry naming the plugin", errs)
	}
}
