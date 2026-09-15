package axiom_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/axiom"
	"github.com/jeremygprawira/wlog/internal/httpdrain"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
)

// newTestDrain points a Drain at a fake ingest server.
func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...axiom.Option) *axiom.Drain {
	t.Helper()
	all := append([]axiom.Option{
		axiom.WithToken("secret-token"),
		axiom.WithDataset("logs"),
		axiom.WithURL(srv.URL),
	}, opts...)
	d, err := axiom.New(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// TestAxiom_SendBatch_NDJSON proves one batch becomes one NDJSON request to the ingest
// endpoint, with bearer auth and wlog's identity headers.
func TestAxiom_SendBatch_NDJSON(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	events := []map[string]any{{"level": "info", "operation": "a"}, {"level": "info", "operation": "b"}}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if req.Method != http.MethodPost || req.Path != "/v1/datasets/logs/ingest" {
		t.Errorf("request = %s %s, want POST /v1/datasets/logs/ingest", req.Method, req.Path)
	}
	if got := req.Headers.Get("Authorization"); got != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", got)
	}
	if got := req.Headers.Get("Content-Type"); got != "application/x-ndjson" {
		t.Errorf("Content-Type = %q, want application/x-ndjson", got)
	}
	if got := req.Headers.Get("X-Wlog-Source"); got != "axiom" {
		t.Errorf("X-Wlog-Source = %q, want axiom", got)
	}
	if got := req.Headers.Get("User-Agent"); !strings.HasPrefix(got, "wlog/") {
		t.Errorf("User-Agent = %q, want a wlog/ prefix", got)
	}
	lines := strings.Split(strings.TrimSpace(string(req.Body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("body has %d lines, want 2: %q", len(lines), req.Body)
	}
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("body line is not JSON: %v (%q)", err, line)
		}
	}
}

// TestAxiom_EnvAlone proves the drain is fully configured by AXIOM_* env vars.
func TestAxiom_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("AXIOM_TOKEN", "env-token")
	t.Setenv("AXIOM_DATASET", "env-dataset")
	t.Setenv("AXIOM_URL", srv.URL)

	d, err := axiom.New()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Headers.Get("Authorization"); got != "Bearer env-token" {
		t.Errorf("Authorization = %q, want Bearer env-token", got)
	}
	if got := srv.Last().Path; got != "/v1/datasets/env-dataset/ingest" {
		t.Errorf("path = %q, want /v1/datasets/env-dataset/ingest", got)
	}
}

// TestAxiom_MissingConfig proves a missing token or dataset is a construction error.
func TestAxiom_MissingConfig(t *testing.T) {
	if _, err := axiom.New(axiom.WithToken(""), axiom.WithDataset("")); err == nil {
		t.Fatal("New with no token and no dataset returned nil error")
	}
	if _, err := axiom.New(axiom.WithToken("t"), axiom.WithDataset("")); err == nil {
		t.Fatal("New with no dataset returned nil error")
	}
}

// TestAxiom_StatusRetryable proves 401 and 403 are non-retryable, and 5xx is.
func TestAxiom_StatusRetryable(t *testing.T) {
	cases := []struct {
		status    int
		retryable bool
	}{
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
	}
	for _, tc := range cases {
		srv := httpfake.New()
		srv.SetStatus(tc.status)
		d := newTestDrain(t, srv)
		err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}})
		srv.Close()

		var statusErr *httpdrain.StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("status %d: err = %v, want a *StatusError", tc.status, err)
		}
		if statusErr.Retryable() != tc.retryable {
			t.Errorf("status %d: Retryable() = %v, want %v", tc.status, statusErr.Retryable(), tc.retryable)
		}
	}
}

// TestAxiom_NeverLeaksRedactedValue proves gate G1 end to end: a value the default
// redactor denies never reaches the wire.
func TestAxiom_NeverLeaksRedactedValue(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)
	drain := pipeline.Wrap(d, pipeline.BatchSize(1))

	log := wlog.New(wlog.WithDrains(drain))
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "password", "hunter2")
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, req := range srv.Requests() {
		if strings.Contains(string(req.Body), "hunter2") {
			t.Errorf("raw denied value reached the drain: %s", req.Body)
		}
	}
	if len(srv.Requests()) == 0 {
		t.Fatal("no request reached the fake, so the leak check proved nothing")
	}
}
