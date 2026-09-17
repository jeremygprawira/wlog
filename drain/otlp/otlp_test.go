package otlp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/otlp"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// goldenEvent is the fixed event the golden file pins.
func goldenEvent() map[string]any {
	return map[string]any{
		"timestamp":   "2026-09-16T12:00:00.123456789Z",
		"level":       "error",
		"operation":   "order.create",
		"outcome":     "error",
		"duration_ms": 12,
		"service":     map[string]any{"name": "checkout", "version": "1.4.0", "env": "prod"},
		"http":        map[string]any{"method": "POST", "status": 500},
		"order_id":    "ord-1",
		"amount":      12.5,
		"paid":        true,
		"tags":        []any{"a", "b"},
		"trace":       map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736", "span_id": "00f067aa0ba902b7"},
	}
}

func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...otlp.Option) *otlp.Sender {
	t.Helper()
	all := append([]otlp.Option{otlp.WithEndpoint(srv.URL)}, opts...)
	d, err := otlp.NewSender(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// prettyBody re-indents the request body the same way the golden file is stored.
func prettyBody(t *testing.T, body []byte) string {
	t.Helper()
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("request body is not JSON: %v\n%s", err, body)
	}
	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		t.Fatalf("re-indent body: %v", err)
	}
	return string(pretty) + "\n"
}

// scopeVersion matches the scope version field of a payload, which follows
// the build of the library.
var scopeVersion = regexp.MustCompile(`"version": "[^"]*"`)

// TestOTLP_SendBatch_Golden proves the mapping matches the pinned golden payload.
//
// The golden file is written by hand from the OTLP/HTTP JSON specification, never produced
// by this encoder: a golden the code generates can only ever agree with the code.
func TestOTLP_SendBatch_Golden(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	if err := d.SendBatch(context.Background(), []map[string]any{goldenEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if req.Path != "/v1/logs" {
		t.Errorf("path = %q, want /v1/logs", req.Path)
	}
	if got := req.Headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := req.Headers.Get("X-Wlog-Source"); got != "otlp" {
		t.Errorf("X-Wlog-Source = %q, want otlp", got)
	}

	got := prettyBody(t, req.Body)
	path := filepath.Join("testdata", "export.golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	// The scope version follows the build, so both sides carry a placeholder in
	// that one field. The test then never compares a version literal.
	got = scopeVersion.ReplaceAllString(got, `"version": "<version>"`)
	body := scopeVersion.ReplaceAllString(string(want), `"version": "<version>"`)
	if got != body {
		t.Errorf("payload does not match %s\n--- got ---\n%s\n--- want ---\n%s", path, got, body)
	}
}

// TestOTLP_Severity proves each wlog level maps to the OTLP severity number and text.
func TestOTLP_Severity(t *testing.T) {
	cases := []struct {
		level  string
		number string
		text   string
	}{
		{"debug", "5", "DEBUG"},
		{"info", "9", "INFO"},
		{"warn", "13", "WARN"},
		{"error", "17", "ERROR"},
	}
	for _, tc := range cases {
		srv := httpfake.New()
		d := newTestDrain(t, srv)
		event := map[string]any{"level": tc.level, "operation": "op"}
		if err := d.SendBatch(context.Background(), []map[string]any{event}); err != nil {
			t.Fatalf("SendBatch: %v", err)
		}
		var body struct {
			ResourceLogs []struct {
				ScopeLogs []struct {
					LogRecords []struct {
						SeverityNumber json.Number `json:"severityNumber"`
						SeverityText   string      `json:"severityText"`
					} `json:"logRecords"`
				} `json:"scopeLogs"`
			} `json:"resourceLogs"`
		}
		if err := json.Unmarshal(srv.Last().Body, &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		record := body.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
		srv.Close()
		if record.SeverityNumber.String() != tc.number || record.SeverityText != tc.text {
			t.Errorf("level %s mapped to %s/%s, want %s/%s", tc.level, record.SeverityNumber, record.SeverityText, tc.number, tc.text)
		}
	}
}

// TestOTLP_EnvHeadersAndEndpoint proves OTEL_EXPORTER_OTLP_ENDPOINT and
// OTEL_EXPORTER_OTLP_HEADERS configure the drain.
func TestOTLP_EnvHeadersAndEndpoint(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-Api-Key=abc,X-Other=1")

	d, err := otlp.NewSender()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info", "operation": "op"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	if req.Path != "/v1/logs" {
		t.Errorf("path = %q, want /v1/logs", req.Path)
	}
	if got := req.Headers.Get("X-Api-Key"); got != "abc" {
		t.Errorf("X-Api-Key = %q, want abc", got)
	}
	if got := req.Headers.Get("X-Other"); got != "1" {
		t.Errorf("X-Other = %q, want 1", got)
	}
}

// TestOTLP_ExplicitOptionWins proves an explicit endpoint and headers win over env.
func TestOTLP_ExplicitOptionWins(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://env.invalid")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-From=env")

	d, err := otlp.NewSender(otlp.WithEndpoint(srv.URL), otlp.WithHeaders(map[string]string{"X-From": "code"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Headers.Get("X-From"); got != "code" {
		t.Errorf("X-From = %q, want code", got)
	}
}

// TestOTLP_NeverLeaksRedactedValue proves gate G1 end to end.
func TestOTLP_NeverLeaksRedactedValue(t *testing.T) {
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

// TestOTLP_PIPE14_HeaderDecode proves a percent-encoded header value is decoded, so a
// bearer token with a space reaches the collector instead of a 401.
func TestOTLP_PIPE14_HeaderDecode(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic%20dXNlcjpwYXNz,X%2DKey=1")

	d, err := otlp.NewSender()
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{goldenEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if got := req.Headers.Get("Authorization"); got != "Basic dXNlcjpwYXNz" {
		t.Errorf("Authorization = %q, want the decoded value", got)
	}
	if got := req.Headers.Get("X-Key"); got != "1" {
		t.Errorf("X-Key = %q, want 1 (the encoded key name decoded too)", got)
	}
}

// TestOTLP_PIPE14_LogsEndpoint proves the logs endpoint env var is used as it stands.
func TestOTLP_PIPE14_LogsEndpoint(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", srv.URL+"/custom/logs")

	d, err := otlp.NewSender()
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{goldenEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Path; got != "/custom/logs" {
		t.Errorf("path = %q, want the env endpoint as it stands", got)
	}
}

// TestOTLP_PIPE14_KvlistInArray proves an object inside an array becomes a kvlistValue, so
// errors[] and logs[] arrive with their fields instead of as empty arrays.
func TestOTLP_PIPE14_KvlistInArray(t *testing.T) {
	event := goldenEvent()
	event["errors"] = []any{
		map[string]any{"code": "PAYMENT_DECLINED", "message": "declined"},
	}
	body, err := otlp.Encode([]map[string]any{event})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if !strings.Contains(string(body), `"kvlistValue"`) {
		t.Fatalf("the object inside the array was dropped:\n%s", body)
	}
	if !strings.Contains(string(body), `"PAYMENT_DECLINED"`) || !strings.Contains(string(body), `"declined"`) {
		t.Errorf("the object's fields are missing:\n%s", body)
	}
}

// TestOTLP_StatusTable proves every status class maps to the right outcome.
func TestOTLP_StatusTable(t *testing.T) {
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
		d, err := otlp.NewSender(otlp.WithEndpoint(srv.URL))
		if err != nil {
			t.Fatalf("NewSender: %v", err)
		}
		sendErr := d.SendBatch(context.Background(), []map[string]any{goldenEvent()})
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
