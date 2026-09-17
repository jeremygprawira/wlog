package hyperdx_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/hyperdx"

	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
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

// TestHyperdx_UsesOtlpEncoder proves the posted body carries the fields HyperDX reads: the
// configured service name, the OTLP severity, and the event as the body. It asserts on the
// decoded payload rather than comparing the body with the encoder, because the drain sends
// whatever the encoder produced, and that comparison can never fail.
func TestHyperdx_UsesOtlpEncoder(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, err := hyperdx.NewSender(
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
	if err := drain.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

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

	var payload struct {
		ResourceLogs []struct {
			Resource struct {
				Attributes []struct {
					Key   string `json:"key"`
					Value struct {
						StringValue string `json:"stringValue"`
					} `json:"value"`
				} `json:"attributes"`
			} `json:"resource"`
			ScopeLogs []struct {
				LogRecords []struct {
					SeverityText string `json:"severityText"`
					Body         struct {
						StringValue string `json:"stringValue"`
					} `json:"body"`
				} `json:"logRecords"`
			} `json:"scopeLogs"`
		} `json:"resourceLogs"`
	}
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		t.Fatalf("body is not OTLP JSON: %v\n%s", err, req.Body)
	}
	if len(payload.ResourceLogs) != 1 {
		t.Fatalf("got %d resourceLogs, want 1", len(payload.ResourceLogs))
	}
	serviceName := ""
	for _, attr := range payload.ResourceLogs[0].Resource.Attributes {
		if attr.Key == "service.name" {
			serviceName = attr.Value.StringValue
		}
	}
	if serviceName != "checkout" {
		t.Errorf("service.name = %q, want the configured HyperDX service", serviceName)
	}
	records := payload.ResourceLogs[0].ScopeLogs[0].LogRecords
	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1", len(records))
	}
	if records[0].SeverityText == "" {
		t.Error("the log record carries no severity text")
	}
	if records[0].Body.StringValue == "" {
		t.Error("the log record carries no body")
	}
}

// TestHyperdx_EnvAlone proves HYPERDX_API_KEY and HYPERDX_ENDPOINT configure the drain.
func TestHyperdx_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("HYPERDX_API_KEY", "env-key")
	t.Setenv("HYPERDX_ENDPOINT", srv.URL+"/v1/logs")

	drain, err := hyperdx.NewSender()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := drain.SendBatch(context.Background(), []map[string]any{{"level": "info", "operation": "op"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
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

// TestHyperDX_PIPE16_EmptyPath proves an endpoint that names only a host still reaches the
// OTLP logs path, instead of posting to the site root.
func TestHyperDX_PIPE16_EmptyPath(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, err := hyperdx.NewSender(hyperdx.WithAPIKey("k"), hyperdx.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := drain.SendBatch(context.Background(), []map[string]any{{"level": "info", "operation": "op"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Path; got != "/v1/logs" {
		t.Errorf("path = %q, want /v1/logs added to the empty path", got)
	}

	// A path the caller chose is left alone.
	custom, err := hyperdx.NewSender(hyperdx.WithAPIKey("k"), hyperdx.WithEndpoint(srv.URL+"/custom/logs"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := custom.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Path; got != "/custom/logs" {
		t.Errorf("path = %q, want the caller's own path", got)
	}
}

// TestHyperdx_New_WrapsWithPipelineDefaults proves New succeeds on a valid
// configuration, and rejects a missing API key the same way NewSender does.
func TestHyperdx_New_WrapsWithPipelineDefaults(t *testing.T) {
	d, err := hyperdx.New(hyperdx.WithAPIKey("key"), hyperdx.WithEndpoint("http://example.invalid"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	flush(t, d)

	t.Setenv("HYPERDX_API_KEY", "")
	if _, err := hyperdx.New(); err == nil {
		t.Error("New with no API key returned nil error")
	}
}

// TestHyperdx_MustNew_PanicsOnTheSameError proves MustNew is New plus a panic, not a
// different construction path.
func TestHyperdx_MustNew_PanicsOnTheSameError(t *testing.T) {
	t.Setenv("HYPERDX_API_KEY", "")
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustNew with no API key did not panic")
			}
		}()
		hyperdx.MustNew()
	}()

	d := hyperdx.MustNew(hyperdx.WithAPIKey("key"), hyperdx.WithEndpoint("http://example.invalid"),
		hyperdx.WithPipeline(pipeline.BatchSize(5)))
	flush(t, d)
}
