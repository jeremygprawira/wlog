package sentry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/sentry"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// dsnFor builds a Sentry DSN pointing at a fake server.
func dsnFor(srv *httpfake.Server) string {
	return "http://public-key@" + strings.TrimPrefix(srv.URL, "http://") + "/42"
}

// newTestDrain points a Sender at a fake ingest server.
func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...sentry.Option) *sentry.Sender {
	t.Helper()
	all := append([]sentry.Option{sentry.WithDSN(dsnFor(srv))}, opts...)
	d, err := sentry.NewSender(all...)
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
		_ = json.Unmarshal(payloads[0], &payload)
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

	d, err := sentry.NewSender()
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
	if _, err := sentry.NewSender(); err == nil {
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

// TestSentry_PIPE7_EnvelopeEventID proves a batch of errors becomes one envelope per
// error, and that each envelope header's event_id equals its item's id. Sentry allows one
// event item per envelope, so a batch that carried two got a 4xx and the whole batch was
// dropped.
func TestSentry_PIPE7_EnvelopeEventID(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	events := []map[string]any{
		errorEvent("PAYMENT_DECLINED", "payment"),
		errorEvent("ORDER_MISSING", "order"),
	}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	requests := srv.Requests()
	if len(requests) != 2 {
		t.Fatalf("the sender made %d requests, want one envelope per error event", len(requests))
	}
	for i, req := range requests {
		header, headers, payloads := parseEnvelope(t, req.Body)
		if len(headers) != 1 || headers[0]["type"] != "event" {
			t.Fatalf("envelope %d holds %v, want exactly one event item", i, headers)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloads[0], &payload); err != nil {
			t.Fatalf("envelope %d payload is not JSON: %v", i, err)
		}
		if header["event_id"] != payload["event_id"] {
			t.Errorf("envelope %d: header event_id %v, item event_id %v; they must match", i, header["event_id"], payload["event_id"])
		}
	}
}

// TestSentry_PIPE8_ErrorsNotLogs proves an error event is never also sent as a log item,
// and that a log item holds flat typed attributes and a trace id.
func TestSentry_PIPE8_ErrorsNotLogs(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, sentry.WithAllEvents(true))

	events := []map[string]any{
		errorEvent("PAYMENT_DECLINED", "payment"),
		{
			"level":     "info",
			"operation": "job.run",
			"order_id":  "ord-1",
			"attempt":   2,
			"trace":     map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"},
		},
	}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	var logItems []any
	for _, req := range srv.Requests() {
		_, headers, payloads := parseEnvelope(t, req.Body)
		for i, header := range headers {
			if header["type"] != "log" {
				continue
			}
			var body map[string]any
			if err := json.Unmarshal(payloads[i], &body); err != nil {
				t.Fatalf("log payload is not JSON: %v", err)
			}
			items, _ := body["items"].([]any)
			logItems = append(logItems, items...)
		}
	}
	if len(logItems) != 1 {
		t.Fatalf("the sender sent %d log items, want only the non-error event", len(logItems))
	}

	item, _ := logItems[0].(map[string]any)
	if item["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("log trace_id = %v, want the event's trace id", item["trace_id"])
	}
	attrs, _ := item["attributes"].(map[string]any)
	orderID, _ := attrs["order_id"].(map[string]any)
	if orderID["value"] != "ord-1" || orderID["type"] != "string" {
		t.Errorf("order_id attribute = %v, want a typed {value, type} pair", attrs["order_id"])
	}
	attempt, _ := attrs["attempt"].(map[string]any)
	if attempt["value"] != float64(2) || attempt["type"] != "integer" {
		t.Errorf("attempt attribute = %v, want a typed integer", attrs["attempt"])
	}
	if _, wholeEvent := attrs["wlog"]; wholeEvent {
		t.Error("attributes still hold the whole event as one untyped object")
	}
}

// TestSentry_PIPE21_DSNForms proves a DSN with a path prefix, and one with no scheme, build
// the endpoint Sentry expects.
func TestSentry_PIPE21_DSNForms(t *testing.T) {
	// A DSN with a path prefix keeps the prefix before /api/.
	srv := httpfake.New()
	defer srv.Close()
	prefixDSN := "http://public-key@" + strings.TrimPrefix(srv.URL, "http://") + "/tenant/one/42"
	d, err := sentry.NewSender(sentry.WithDSN(prefixDSN))
	if err != nil {
		t.Fatalf("New with a path prefix: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{errorEvent("PAYMENT_DECLINED", "payment")}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if req := srv.Last(); req == nil || req.Path != "/tenant/one/api/42/envelope/" {
		t.Errorf("path = %v, want /tenant/one/api/42/envelope/", req)
	}

	// A DSN without a scheme is allowed by the spec, so it must parse.
	noScheme := "public-key@" + strings.TrimPrefix(srv.URL, "http://") + "/42"
	if _, err := sentry.NewSender(sentry.WithDSN(noScheme)); err != nil {
		t.Errorf("New with no scheme: %v", err)
	}
}

// TestSentry_StatusTable proves every status class the plan names maps to the right
// outcome, so a drain never retries a refused request forever.
func TestSentry_StatusTable(t *testing.T) {
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
		d := newTestDrain(t, srv)
		err := d.SendBatch(context.Background(), []map[string]any{errorEvent("PAYMENT_DECLINED", "payment")})
		srv.Close()

		if tc.wantError && err == nil {
			t.Errorf("status %d: err = nil, want an error", tc.status)
			continue
		}
		if !tc.wantError {
			if err != nil {
				t.Errorf("status %d: err = %v, want nil", tc.status, err)
			}
			continue
		}
		var statusErr *httpdrain.StatusError
		if !errors.As(err, &statusErr) {
			t.Errorf("status %d: err = %v, want a *StatusError", tc.status, err)
			continue
		}
		if statusErr.Retryable() != tc.retryable {
			t.Errorf("status %d: Retryable() = %v, want %v", tc.status, statusErr.Retryable(), tc.retryable)
		}
	}
}
