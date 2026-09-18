package wlog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// multiPlugin implements every optional plugin hook, to prove one struct can serve as
// setup, enricher, sampler, and drain at once.
type multiPlugin struct {
	setupCalled bool
	sent        []map[string]any
}

func (p *multiPlugin) Name() string { return "multi" }
func (p *multiPlugin) Setup(l *wlog.Logger) error {
	p.setupCalled = true
	return nil
}
func (p *multiPlugin) Enrich(_ context.Context, event map[string]any) { event["tenant"] = "acme" }
func (p *multiPlugin) Keep(context.Context, map[string]any) bool      { return true }
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
