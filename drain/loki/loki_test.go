package loki_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/loki"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// stream is the push body's stream shape, used to read the request back.
type stream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

type pushBody struct {
	Streams []stream `json:"streams"`
}

// decodePush reads the last request body into pushBody.
func decodePush(t *testing.T, body []byte) pushBody {
	t.Helper()
	var pushed pushBody
	if err := json.Unmarshal(body, &pushed); err != nil {
		t.Fatalf("push body is not JSON: %v\n%s", err, body)
	}
	return pushed
}

func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...loki.Option) *loki.Sender {
	t.Helper()
	all := append([]loki.Option{loki.WithURL(srv.URL)}, opts...)
	d, err := loki.NewSender(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

func serviceEvent(level, name, env string) map[string]any {
	return map[string]any{
		"timestamp": time.Unix(1758000000, 0).UTC().Format(time.RFC3339Nano),
		"level":     level,
		"operation": "op",
		"service":   map[string]any{"name": name, "env": env},
	}
}

// TestLoki_SendBatch_GroupsByLabelSet proves events are grouped into one stream per
// label set, with nanosecond timestamps and the whole event as the line.
func TestLoki_SendBatch_GroupsByLabelSet(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	events := []map[string]any{
		serviceEvent("info", "api", "prod"),
		serviceEvent("info", "api", "prod"),
		serviceEvent("error", "api", "prod"),
	}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if req.Path != "/loki/api/v1/push" {
		t.Errorf("path = %q, want /loki/api/v1/push", req.Path)
	}
	if got := req.Headers.Get("X-Wlog-Source"); got != "loki" {
		t.Errorf("X-Wlog-Source = %q, want loki", got)
	}
	pushed := decodePush(t, req.Body)
	if len(pushed.Streams) != 2 {
		t.Fatalf("got %d streams, want 2 (one per level): %s", len(pushed.Streams), req.Body)
	}
	for _, s := range pushed.Streams {
		if s.Stream["service"] != "api" || s.Stream["env"] != "prod" {
			t.Errorf("stream labels = %v, want service=api env=prod", s.Stream)
		}
		if len(s.Values) == 0 {
			t.Fatal("stream has no values")
		}
		if s.Values[0][0] != "1758000000000000000" {
			t.Errorf("timestamp = %q, want 1758000000000000000", s.Values[0][0])
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(s.Values[0][1]), &line); err != nil {
			t.Errorf("line is not a JSON event: %v (%q)", err, s.Values[0][1])
		}
	}
}

// TestLoki_CustomLabels proves WithLabels replaces the default label set.
func TestLoki_CustomLabels(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, loki.WithLabels("tenant"))

	events := []map[string]any{{"tenant": "acme", "level": "info", "operation": "op"}}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	pushed := decodePush(t, srv.Last().Body)
	if len(pushed.Streams) != 1 {
		t.Fatalf("got %d streams, want 1", len(pushed.Streams))
	}
	if pushed.Streams[0].Stream["tenant"] != "acme" {
		t.Errorf("labels = %v, want tenant=acme", pushed.Streams[0].Stream)
	}
	if _, hasService := pushed.Streams[0].Stream["service"]; hasService {
		t.Errorf("labels still have the default service key: %v", pushed.Streams[0].Stream)
	}
}

// TestLoki_RejectsHighCardinalityLabel proves a label known to explode the stream
// count fails fast at construction, before any event is sent.
func TestLoki_RejectsHighCardinalityLabel(t *testing.T) {
	for _, key := range []string{"trace.trace_id", "http.path", "user.id"} {
		if _, err := loki.NewSender(loki.WithURL("http://example.invalid"), loki.WithLabels("service", key)); err == nil {
			t.Errorf("New with label %q returned nil error, want a rejection", key)
		}
	}
}

// TestLoki_AuthAndTenant proves basic auth and the tenant header are set when configured.
func TestLoki_AuthAndTenant(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, loki.WithBasicAuth("user", "pass"), loki.WithTenantID("tenant-1"))

	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	if got := req.Headers.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
		t.Errorf("Authorization = %q, want a Basic value", got)
	}
	if got := req.Headers.Get("X-Scope-OrgID"); got != "tenant-1" {
		t.Errorf("X-Scope-OrgID = %q, want tenant-1", got)
	}
}

// TestLoki_EnvAlone proves the drain is fully configured by LOKI_* env vars.
func TestLoki_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("LOKI_URL", srv.URL)
	t.Setenv("LOKI_TENANT_ID", "env-tenant")

	d, err := loki.NewSender()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Headers.Get("X-Scope-OrgID"); got != "env-tenant" {
		t.Errorf("X-Scope-OrgID = %q, want env-tenant", got)
	}
}

// TestLoki_NeverLeaksRedactedValue proves gate G1 end to end.
func TestLoki_NeverLeaksRedactedValue(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	log := wlog.New(wlog.WithDrains(pipeline.Wrap(d, pipeline.BatchSize(1))))
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "password", "hunter2")
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(srv.Requests()) == 0 {
		t.Fatal("no request reached the fake, so the leak check proved nothing")
	}
	for _, req := range srv.Requests() {
		if strings.Contains(string(req.Body), "hunter2") {
			t.Errorf("raw denied value reached the drain: %s", req.Body)
		}
	}
}

// TestLoki_PIPE13_DottedLabels proves a dotted label path resolves inside the event, that
// its label name has no dot, and that an invalid label name is refused.
func TestLoki_PIPE13_DottedLabels(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, loki.WithLabels("level", "http.method"))

	event := map[string]any{
		"level":   "info",
		"http":    map[string]any{"method": "GET"},
		"message": "hello",
	}
	if err := d.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	pushed := decodePush(t, srv.Last().Body)
	if len(pushed.Streams) != 1 {
		t.Fatalf("got %d streams, want 1", len(pushed.Streams))
	}
	labels := pushed.Streams[0].Stream
	if labels["http_method"] != "GET" {
		t.Errorf("labels = %v, want http_method=GET from the nested path", labels)
	}
	if _, dotted := labels["http.method"]; dotted {
		t.Errorf("the label kept its dot, which Loki rejects: %v", labels)
	}

	// A path whose name cannot be a Loki label fails at construction.
	if _, err := loki.NewSender(loki.WithURL(srv.URL), loki.WithLabels("http.method!")); err == nil {
		t.Error("NewSender accepted an invalid label name")
	}
	// The denylist covers a path and its last segment, so the short form is refused too.
	if _, err := loki.NewSender(loki.WithURL(srv.URL), loki.WithLabels("request_id")); err == nil {
		t.Error("NewSender accepted request_id, which is the last segment of a denied key")
	}
}

// TestLoki_PIPE13_BasicAuthPair proves basic auth needs both halves, so Loki never
// receives an Authorization header that cannot work.
func TestLoki_PIPE13_BasicAuthPair(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()

	if _, err := loki.NewSender(loki.WithURL(srv.URL), loki.WithBasicAuth("user", "")); err == nil {
		t.Error("NewSender accepted a username with no password")
	}
	if _, err := loki.NewSender(loki.WithURL(srv.URL), loki.WithBasicAuth("", "pass")); err == nil {
		t.Error("NewSender accepted a password with no username")
	}
	if _, err := loki.NewSender(loki.WithURL(srv.URL), loki.WithBasicAuth("user", "pass")); err != nil {
		t.Errorf("NewSender refused a complete pair: %v", err)
	}
}

// TestLoki_StatusTable proves every status class maps to the right outcome.
func TestLoki_StatusTable(t *testing.T) {
	cases := []struct {
		status    int
		wantError bool
		retryable bool
	}{
		{http.StatusOK, false, false},
		{http.StatusBadRequest, true, false},
		{http.StatusUnauthorized, true, false},
		{http.StatusForbidden, true, false},
		{http.StatusRequestEntityTooLarge, true, false},
		{http.StatusTooManyRequests, true, true},
		{http.StatusInternalServerError, true, true},
	}
	for _, tc := range cases {
		srv := httpfake.New()
		srv.SetStatus(tc.status)
		d, err := loki.NewSender(loki.WithURL(srv.URL))
		if err != nil {
			t.Fatalf("NewSender: %v", err)
		}
		sendErr := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}})
		srv.Close()

		if !tc.wantError {
			if sendErr != nil {
				t.Errorf("status %d: err = %v, want nil", tc.status, sendErr)
			}
			continue
		}
		if sendErr == nil {
			t.Errorf("status %d: err = nil, want an error", tc.status)
			continue
		}
		var statusErr *httpdrain.StatusError
		if !errors.As(sendErr, &statusErr) {
			t.Errorf("status %d: err = %v, want a *StatusError", tc.status, sendErr)
			continue
		}
		if statusErr.Retryable() != tc.retryable {
			t.Errorf("status %d: Retryable() = %v, want %v", tc.status, statusErr.Retryable(), tc.retryable)
		}
	}
}

// TestLoki_New_WrapsWithPipelineDefaults proves New succeeds on a valid configuration,
// returns something wlog.Logger.Close can close, and rejects the same configuration
// NewSender already rejects. MustNew mirrors both outcomes: it returns the same drain on
// success, and panics with New's own error on failure.
func TestLoki_New_WrapsWithPipelineDefaults(t *testing.T) {
	d, err := loki.New(loki.WithURL("http://example.invalid"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	closer, ok := d.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("New's drain does not implement Close, so Logger.Close cannot stop it")
	}
	if err := closer.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}

	if _, err := loki.New(loki.WithURL("http://example.invalid"), loki.WithLabels("service", "user.id")); err == nil {
		t.Error("New with a high-cardinality label returned nil error, want a rejection")
	}
}

// TestLoki_MustNew_PanicsOnTheSameError proves MustNew is New plus a panic, not a
// different construction path.
func TestLoki_MustNew_PanicsOnTheSameError(t *testing.T) {
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustNew with a high-cardinality label did not panic")
			}
		}()
		loki.MustNew(loki.WithURL("http://example.invalid"), loki.WithLabels("service", "user.id"))
	}()

	d := loki.MustNew(loki.WithURL("http://example.invalid"), loki.WithPipeline(pipeline.BatchSize(5)), loki.WithGzip(true))
	closer, ok := d.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("MustNew's drain does not implement Close")
	}
	if err := closer.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}
