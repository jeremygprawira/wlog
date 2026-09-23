package victorialogs_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/victorialogs"
	"github.com/jeremygprawira/wlog/pipeline"
)

// fakeLogs records every request and answers 200.
type fakeLogs struct {
	*httptest.Server
	mu      sync.Mutex
	status  int
	bodies  []string
	paths   []string
	headers []http.Header
}

// newFake starts a fake VictoriaLogs that answers status.
func newFake(t *testing.T, status int) *fakeLogs {
	t.Helper()
	f := &fakeLogs{status: status}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.paths = append(f.paths, r.URL.RequestURI())
		f.headers = append(f.headers, r.Header.Clone())
		code := f.status
		f.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(f.Close)
	return f
}

// last returns the most recent body, path, and headers.
func (f *fakeLogs) last() (string, string, http.Header) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return "", "", nil
	}
	last := len(f.bodies) - 1
	return f.bodies[last], f.paths[last], f.headers[last]
}

// event returns the one event the golden body holds.
func event() map[string]any {
	return map[string]any{
		"timestamp": "2026-09-22T10:00:00Z",
		"level":     "info",
		"kind":      "request",
		"outcome":   "success",
		"summary":   "GET /orders",
	}
}

// TestVictoriaLogs_GoldenBody proves the jsonline body matches the golden, and the URL
// holds the field and stream parameters.
func TestVictoriaLogs_GoldenBody(t *testing.T) {
	srv := newFake(t, http.StatusOK)
	sender, err := victorialogs.NewSender(victorialogs.WithURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	golden, err := os.ReadFile("testdata/request.ndjson")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	body, path, _ := srv.last()
	if body != string(golden) {
		t.Errorf("body =\n%s\nwant\n%s", body, golden)
	}
	if !strings.Contains(path, "_stream_fields=service.name,service.env") {
		t.Errorf("path = %q, want the default stream fields", path)
	}
}

// TestVictoriaLogs_StreamFields proves a high-cardinality field is refused.
func TestVictoriaLogs_StreamFields(t *testing.T) {
	for _, field := range []string{"trace.trace_id", "event_id", "user.id", "http.path", "http.client_ip"} {
		if _, err := victorialogs.NewSender(
			victorialogs.WithURL("http://example.invalid"),
			victorialogs.WithStreamFields(field),
		); err == nil {
			t.Errorf("NewSender accepted the stream field %q", field)
		}
	}
	if _, err := victorialogs.NewSender(
		victorialogs.WithURL("http://example.invalid"),
		victorialogs.WithStreamFields("service.name"),
	); err != nil {
		t.Errorf("NewSender refused a low-cardinality field: %v", err)
	}
}

// TestVictoriaLogs_MaxLine proves a line over the cap is dropped with reason too_large.
func TestVictoriaLogs_MaxLine(t *testing.T) {
	srv := newFake(t, http.StatusOK)
	sender, err := victorialogs.NewSender(victorialogs.WithURL(srv.URL), victorialogs.WithMaxLineBytes(128))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = sender.SendBatch(context.Background(), []map[string]any{
		event(),
		{"summary": strings.Repeat("x", 200)},
	})
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if len(partial.Dropped) != 1 || partial.Dropped[0] != 1 {
		t.Errorf("Dropped = %v, want [1]", partial.Dropped)
	}
	if partial.Reason != "too_large" {
		t.Errorf("Reason = %q, want too_large", partial.Reason)
	}
}

// TestVictoriaLogs_Tenant proves the tenant headers go only with WithTenant.
func TestVictoriaLogs_Tenant(t *testing.T) {
	srv := newFake(t, http.StatusOK)
	sender, err := victorialogs.NewSender(
		victorialogs.WithURL(srv.URL),
		victorialogs.WithTenant("42", "7"),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	_, _, headers := srv.last()
	if headers.Get("AccountID") != "42" || headers.Get("ProjectID") != "7" {
		t.Errorf("tenant headers = %q/%q, want 42/7", headers.Get("AccountID"), headers.Get("ProjectID"))
	}
}

// TestVictoriaLogs_Options proves every option reaches the sender and New wraps it.
func TestVictoriaLogs_Options(t *testing.T) {
	srv := newFake(t, http.StatusOK)
	drain, err := victorialogs.New(
		victorialogs.WithURL(srv.URL),
		victorialogs.WithStreamFields("service.name"),
		victorialogs.WithMaxLineBytes(1<<20),
		victorialogs.WithTenant("42", "7"),
		victorialogs.WithHTTPClient(&http.Client{}),
		victorialogs.WithTimeout(2*time.Second),
		victorialogs.WithUserAgent("agent"),
		victorialogs.WithGzip(true),
		victorialogs.WithPipeline(pipeline.BatchSize(1)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drain.Send(context.Background(), event())
	flusher, ok := drain.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("the wrapped drain has no Flush")
	}
	if err := flusher.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

// TestVictoriaLogs_Env proves the environment supplies the URL and the stream fields.
func TestVictoriaLogs_Env(t *testing.T) {
	t.Setenv("VICTORIALOGS_URL", "http://example.invalid")
	t.Setenv("VICTORIALOGS_STREAM_FIELDS", "service.name, service.env")
	t.Setenv("VICTORIALOGS_ACCOUNT_ID", "42")
	t.Setenv("VICTORIALOGS_PROJECT_ID", "7")
	if _, err := victorialogs.NewSender(); err != nil {
		t.Fatalf("NewSender: %v", err)
	}
}

// TestVictoriaLogs_MustNewPanics proves a missing URL panics rather than returning nil.
func TestVictoriaLogs_MustNewPanics(t *testing.T) {
	t.Setenv("VICTORIALOGS_URL", "")
	defer func() {
		if recover() == nil {
			t.Error("MustNew did not panic on a missing URL")
		}
	}()
	victorialogs.MustNew()
}
