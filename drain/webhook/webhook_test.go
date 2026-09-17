package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/webhook"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
)

func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...webhook.Option) *webhook.Sender {
	t.Helper()
	all := append([]webhook.Option{webhook.WithURL(srv.URL)}, opts...)
	d, err := webhook.NewSender(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// TestWebhook_JSONArray proves the default body is one JSON array with custom headers.
func TestWebhook_JSONArray(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, webhook.WithHeaders(map[string]string{"X-Api-Key": "k1"}))

	events := []map[string]any{{"operation": "a"}, {"operation": "b"}}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if got := req.Headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := req.Headers.Get("X-Api-Key"); got != "k1" {
		t.Errorf("X-Api-Key = %q, want k1", got)
	}
	if got := req.Headers.Get("X-Wlog-Source"); got != "webhook" {
		t.Errorf("X-Wlog-Source = %q, want webhook", got)
	}
	var body []map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("body is not a JSON array: %v\n%s", err, req.Body)
	}
	if len(body) != 2 {
		t.Errorf("body has %d events, want 2: %s", len(body), req.Body)
	}
}

// TestWebhook_NDJSON proves WithNDJSON sends one event per line.
func TestWebhook_NDJSON(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, webhook.WithNDJSON(true))

	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "a"}, {"operation": "b"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	if got := req.Headers.Get("Content-Type"); got != "application/x-ndjson" {
		t.Errorf("Content-Type = %q, want application/x-ndjson", got)
	}
	if got := len(strings.Split(strings.TrimSpace(string(req.Body)), "\n")); got != 2 {
		t.Errorf("body has %d lines, want 2: %s", got, req.Body)
	}
}

// TestWebhook_HMACSigned proves the signature header is the HMAC-SHA256 of the body.
func TestWebhook_HMACSigned(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, webhook.WithSecret("s3cret"))

	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "a"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(req.Body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := req.Headers.Get("X-Wlog-Signature"); got != want {
		t.Errorf("X-Wlog-Signature = %q, want %q", got, want)
	}
}

// TestWebhook_NoSecretNoSignature proves the signature header is absent without a secret.
func TestWebhook_NoSecretNoSignature(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "a"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Headers.Get("X-Wlog-Signature"); got != "" {
		t.Errorf("X-Wlog-Signature = %q, want no header", got)
	}
}

// TestWebhook_EnvAlone proves WLOG_WEBHOOK_URL alone configures the drain.
func TestWebhook_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("WLOG_WEBHOOK_URL", srv.URL)

	d, err := webhook.NewSender()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "env"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if srv.Last() == nil {
		t.Fatal("no request reached the fake")
	}
}

// TestWebhook_MissingURL proves a missing URL is a construction error.
func TestWebhook_MissingURL(t *testing.T) {
	if _, err := webhook.NewSender(); err == nil {
		t.Fatal("New with no URL returned nil error")
	}
}

// TestWebhook_NeverLeaksRedactedValue proves gate G1 end to end.
func TestWebhook_NeverLeaksRedactedValue(t *testing.T) {
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
