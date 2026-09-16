package hyperdx_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/hyperdx"
	"github.com/jeremygprawira/wlog/drain/otlp"
	"github.com/jeremygprawira/wlog/internal/httpfake"
)

// flush closes a wrapped drain, which sends every buffered event.
func flush(t *testing.T, drain wlog.Drain) {
	t.Helper()
	closer, ok := drain.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("drain does not close")
	}
	if err := closer.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestHyperdx_UsesOtlpEncoder proves the posted body equals the OTLP encoder's output for
// the same event with the HyperDX service name, and that the key is the auth header.
func TestHyperdx_UsesOtlpEncoder(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, err := hyperdx.New(
		hyperdx.WithAPIKey("hdx-key"),
		hyperdx.WithEndpoint(srv.URL+"/v1/logs"),
		hyperdx.WithService("checkout"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	event := map[string]any{
		"timestamp": "2026-09-16T12:00:00Z",
		"level":     "info",
		"operation": "job.run",
		"service":   map[string]any{"name": "orders", "version": "1.0.0"},
	}
	drain.Send(context.Background(), event)
	flush(t, drain)

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if req.Path != "/v1/logs" {
		t.Errorf("path = %q, want /v1/logs", req.Path)
	}
	if got := req.Headers.Get("Authorization"); got != "hdx-key" {
		t.Errorf("Authorization = %q, want hdx-key", got)
	}

	want, err := otlp.Encode([]map[string]any{{
		"timestamp": "2026-09-16T12:00:00Z",
		"level":     "info",
		"operation": "job.run",
		"service":   map[string]any{"name": "checkout", "version": "1.0.0"},
	}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(req.Body) != string(want) {
		t.Errorf("HyperDX body differs from the OTLP encoder\n got: %s\nwant: %s", req.Body, want)
	}
}

// TestHyperdx_EnvAlone proves HYPERDX_API_KEY and HYPERDX_ENDPOINT configure the drain.
func TestHyperdx_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("HYPERDX_API_KEY", "env-key")
	t.Setenv("HYPERDX_ENDPOINT", srv.URL+"/v1/logs")

	drain, err := hyperdx.New()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	drain.Send(context.Background(), map[string]any{"level": "info", "operation": "op"})
	flush(t, drain)
	if srv.Last() == nil {
		t.Fatal("no request reached the fake")
	}
}

// TestHyperdx_NeverLeaksRedactedValue proves gate G1 end to end.
func TestHyperdx_NeverLeaksRedactedValue(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, _ := hyperdx.New(hyperdx.WithAPIKey("k"), hyperdx.WithEndpoint(srv.URL+"/v1/logs"))

	log := wlog.New(wlog.WithDrains(drain))
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "password", "hunter2")
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(srv.Requests()) == 0 {
		t.Fatal("no request reached the fake")
	}
	for _, req := range srv.Requests() {
		if strings.Contains(string(req.Body), "hunter2") {
			t.Errorf("raw denied value reached the drain: %s", req.Body)
		}
	}
}
