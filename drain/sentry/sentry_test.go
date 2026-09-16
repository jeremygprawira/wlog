package sentry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/sentry"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
)

// dsnFor builds a Sentry DSN pointing at a fake server.
func dsnFor(srv *httpfake.Server) string {
	return "http://public-key@" + strings.TrimPrefix(srv.URL, "http://") + "/42"
}

// newTestDrain points a Drain at a fake ingest server.
func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...sentry.Option) *sentry.Drain {
	t.Helper()
	all := append([]sentry.Option{sentry.WithDSN(dsnFor(srv))}, opts...)
	d, err := sentry.New(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// errorEvent builds an error-level event with an error detail.
func errorEvent(code, kind string) map[string]any {
	errInfo := map[string]any{"message": "charge declined"}
	if code != "" {
		errInfo["code"] = code
	}
	if kind != "" {
		errInfo["kind"] = kind
	}
	return map[string]any{
		"timestamp": "2026-09-16T12:00:00Z",
		"level":     "error",
		"operation": "payment.charge",
		"service":   map[string]any{"name": "orders", "env": "prod"},
		"error":     errInfo,
	}
}

// parseEnvelope splits the envelope body into its header and items. Each item is a
// header line plus one payload line.
func parseEnvelope(t *testing.T, body []byte) (map[string]any, []map[string]any, [][]byte) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) < 3 || (len(lines)-1)%2 != 0 {
		t.Fatalf("envelope has %d lines, want a header plus item pairs:\n%s", len(lines), body)
	}
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("header is not JSON: %v", err)
	}
	var headers []map[string]any
	var payloads [][]byte
	for i := 1; i+1 < len(lines); i += 2 {
		var itemHeader map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &itemHeader); err != nil {
			t.Fatalf("item header is not JSON: %v", err)
		}
		headers = append(headers, itemHeader)
		payloads = append(payloads, []byte(lines[i+1]))
	}
	return header, headers, payloads
}

// TestSentry_SendBatch_ErrorEnvelope proves an error event becomes one envelope with an
// event item, grouped by the error code, with the whole event attached.
func TestSentry_SendBatch_ErrorEnvelope(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	if err := d.SendBatch(context.Background(), []map[string]any{errorEvent("PAYMENT_DECLINED", "payment")}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if req.Path != "/api/42/envelope/" {
		t.Errorf("path = %q, want /api/42/envelope/", req.Path)
	}
	if got := req.Headers.Get("Content-Type"); got != "application/x-sentry-envelope" {
		t.Errorf("Content-Type = %q, want application/x-sentry-envelope", got)
	}
	if auth := req.Headers.Get("X-Sentry-Auth"); !strings.Contains(auth, "sentry_key=public-key") || !strings.Contains(auth, "sentry_version=7") {
		t.Errorf("X-Sentry-Auth = %q, want key and version", auth)
	}
	if got := req.Headers.Get("X-Wlog-Source"); got != "sentry" {
		t.Errorf("X-Wlog-Source = %q, want sentry", got)
	}

	header, headers, payloads := parseEnvelope(t, req.Body)
	if header["sent_at"] == "" || header["event_id"] == "" {
		t.Errorf("envelope header = %v, want event_id and sent_at", header)
	}
	if len(headers) != 1 || headers[0]["type"] != "event" {
		t.Fatalf("item headers = %v, want one event item", headers)
	}
	if want := float64(len(payloads[0])); headers[0]["length"] != want {
		t.Errorf("item length = %v, want %d", headers[0]["length"], len(payloads[0]))
	}

	var payload map[string]any
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("event payload is not JSON: %v", err)
	}
	fingerprint, _ := payload["fingerprint"].([]any)
	if len(fingerprint) != 1 || fingerprint[0] != "PAYMENT_DECLINED" {
		t.Errorf("fingerprint = %v, want [PAYMENT_DECLINED]", fingerprint)
	}
	contexts, _ := payload["contexts"].(map[string]any)
	if contexts["wlog"] == nil {
		t.Errorf("contexts.wlog missing from %v", payload)
	}
}

// TestSentry_CleanBatchSendsNothing proves a batch with no error event sends no request
// unless AllEvents is on.
func TestSentry_CleanBatchSendsNothing(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	info := map[string]any{"level": "info", "operation": "job.run"}
	if err := d.SendBatch(context.Background(), []map[string]any{info}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if srv.Last() != nil {
		t.Errorf("a clean batch sent a request: %s", srv.Last().Body)
	}
}

// TestSentry_AllEvents_SendsLogs proves the opt-in sends every event as a log item.
func TestSentry_AllEvents_SendsLogs(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, sentry.WithAllEvents(true))

	info := map[string]any{"level": "info", "operation": "job.run", "service": map[string]any{"name": "orders"}}
	if err := d.SendBatch(context.Background(), []map[string]any{info}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	_, headers, payloads := parseEnvelope(t, srv.Last().Body)
	if len(headers) != 1 || headers[0]["type"] != "log" {
		t.Fatalf("item headers = %v, want one log item", headers)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("log payload is not JSON: %v", err)
	}
	items, _ := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("log items = %v, want one", payload["items"])
	}
	item, _ := items[0].(map[string]any)
	if item["body"] != "job.run" || item["level"] != "info" {
		t.Errorf("log item = %v, want body job.run at info", item)
	}
}

// TestSentry_FingerprintFallback proves the fingerprint falls back from code to kind to
// INTERNAL.
func TestSentry_FingerprintFallback(t *testing.T) {
	cases := []struct {
		name string
		code string
		kind string
		want string
	}{
		{"code wins", "PAYMENT_DECLINED", "payment", "PAYMENT_DECLINED"},
		{"kind fallback", "", "payment", "payment"},
		{"internal fallback", "", "", "INTERNAL"},
	}
	for _, tc := range cases {
		srv := httpfake.New()
		d := newTestDrain(t, srv)
		if err := d.SendBatch(context.Background(), []map[string]any{errorEvent(tc.code, tc.kind)}); err != nil {
			t.Fatalf("%s: SendBatch: %v", tc.name, err)
		}
		_, _, payloads := parseEnvelope(t, srv.Last().Body)
		var payload map[string]any
		json.Unmarshal(payloads[0], &payload)
		fingerprint, _ := payload["fingerprint"].([]any)
		srv.Close()
		if len(fingerprint) != 1 || fingerprint[0] != tc.want {
			t.Errorf("%s: fingerprint = %v, want [%s]", tc.name, fingerprint, tc.want)
		}
	}
}

// TestSentry_EnvAlone proves SENTRY_DSN alone configures the drain.
func TestSentry_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("SENTRY_DSN", dsnFor(srv))

	d, err := sentry.New()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{errorEvent("X", "")}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if srv.Last() == nil {
		t.Fatal("no request reached the fake")
	}
}

// TestSentry_MissingDSN proves a missing DSN is a construction error.
func TestSentry_MissingDSN(t *testing.T) {
	if _, err := sentry.New(); err == nil {
		t.Fatal("New with no DSN returned nil error")
	}
}

// TestSentry_NeverLeaksRedactedValue proves gate G1 end to end.
func TestSentry_NeverLeaksRedactedValue(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	log := wlog.New(wlog.WithDrains(pipeline.Wrap(d, pipeline.BatchSize(1))))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "password", "hunter2")
	wlog.Error(ctx, context.DeadlineExceeded)
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
