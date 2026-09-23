package honeycomb_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/honeycomb"
	"github.com/jeremygprawira/wlog/pipeline"
)

// fakeHoneycomb records every request body and answers with a fixed body.
type fakeHoneycomb struct {
	*httptest.Server
	mu      sync.Mutex
	status  int
	answer  string
	bodies  []string
	headers []http.Header
}

// newFake starts a fake Honeycomb that answers with status and answer.
func newFake(t *testing.T, status int, answer string) *fakeHoneycomb {
	t.Helper()
	f := &fakeHoneycomb{status: status, answer: answer}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.headers = append(f.headers, r.Header.Clone())
		code, reply := f.status, f.answer
		f.mu.Unlock()
		w.WriteHeader(code)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(f.Close)
	return f
}

// last returns the most recent request body and headers.
func (f *fakeHoneycomb) last() (string, http.Header) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return "", nil
	}
	return f.bodies[len(f.bodies)-1], f.headers[len(f.headers)-1]
}

// requestEvent returns the one event the golden body holds.
func requestEvent() map[string]any {
	return map[string]any{
		"timestamp": "2026-09-22T10:00:00Z",
		"level":     "info",
		"summary":   "GET /orders",
		"operation": "GET /orders",
		"kind":      "request",
		"outcome":   "success",
		"service":   map[string]any{"name": "checkout"},
		"trace":     map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736", "span_id": "00f067aa0ba902b7"},
		"http":      map[string]any{"method": "GET", "route": "/orders", "status": int64(200)},
	}
}

// TestHoneycomb_GoldenBody proves the request body matches the golden written from the
// Honeycomb documents.
func TestHoneycomb_GoldenBody(t *testing.T) {
	srv := newFake(t, http.StatusOK, `[{"status":202}]`)
	sender, err := honeycomb.NewSender(honeycomb.WithAPIKey("key"), honeycomb.WithAPIURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	golden, err := os.ReadFile("testdata/request.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	body, headers := srv.last()
	if body != strings.TrimRight(string(golden), "\n") {
		t.Errorf("request body =\n%s\nwant\n%s", body, golden)
	}
	if headers.Get("X-Honeycomb-Team") != "key" {
		t.Errorf("X-Honeycomb-Team = %q, want key", headers.Get("X-Honeycomb-Team"))
	}
}

// TestHoneycomb_PartialStatuses proves the positional statuses map to the exact Retry
// and Dropped sets.
func TestHoneycomb_PartialStatuses(t *testing.T) {
	srv := newFake(t, http.StatusOK, `[{"status":202},{"status":503},{"status":400}]`)
	sender, err := honeycomb.NewSender(
		honeycomb.WithAPIKey("key"),
		honeycomb.WithAPIURL(srv.URL),
		honeycomb.WithDataset("logs"),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = sender.SendBatch(context.Background(), []map[string]any{
		{"kind": "request"}, {"kind": "request"}, {"kind": "request"},
	})
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if len(partial.Retry) != 1 || partial.Retry[0] != 1 {
		t.Errorf("Retry = %v, want [1]", partial.Retry)
	}
	if len(partial.Dropped) != 1 || partial.Dropped[0] != 2 {
		t.Errorf("Dropped = %v, want [2]", partial.Dropped)
	}
	if partial.Reason != "status_400" {
		t.Errorf("Reason = %q, want status_400", partial.Reason)
	}
}

// count returns how many requests arrived.
func (f *fakeHoneycomb) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

// TestHoneycomb_Options proves every option reaches the sender and New wraps it.
func TestHoneycomb_Options(t *testing.T) {
	srv := newFake(t, http.StatusOK, `[{"status":202}]`)
	drain, err := honeycomb.New(
		honeycomb.WithAPIKey("key"),
		honeycomb.WithDataset("logs"),
		honeycomb.WithAPIURL(srv.URL),
		honeycomb.WithSpans(false),
		honeycomb.WithHTTPClient(&http.Client{}),
		honeycomb.WithTimeout(2*time.Second),
		honeycomb.WithUserAgent("agent"),
		honeycomb.WithGzip(true),
		honeycomb.WithPipeline(pipeline.BatchSize(1)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drain.Send(context.Background(), requestEvent())
	flusher, ok := drain.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("the wrapped drain has no Flush")
	}
	if err := flusher.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if srv.count() == 0 {
		t.Error("no request arrived")
	}
}

// TestHoneycomb_Env proves the environment supplies the key, the dataset, and the URL.
func TestHoneycomb_Env(t *testing.T) {
	t.Setenv("HONEYCOMB_API_KEY", "envkey")
	t.Setenv("HONEYCOMB_DATASET", "envdataset")
	t.Setenv("HONEYCOMB_API_URL", "http://example.invalid")
	if _, err := honeycomb.NewSender(); err != nil {
		t.Fatalf("NewSender: %v", err)
	}
}

// TestHoneycomb_MustNewPanics proves a missing key panics rather than returning nil.
func TestHoneycomb_MustNewPanics(t *testing.T) {
	t.Setenv("HONEYCOMB_API_KEY", "")
	defer func() {
		if recover() == nil {
			t.Error("MustNew did not panic on a missing key")
		}
	}()
	honeycomb.MustNew()
}

// TestHoneycomb_DatasetSplit proves the batch splits per dataset when no dataset is set.
func TestHoneycomb_DatasetSplit(t *testing.T) {
	srv := newFake(t, http.StatusOK, `[{"status":202}]`)
	sender, err := honeycomb.NewSender(honeycomb.WithAPIKey("key"), honeycomb.WithAPIURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	second := requestEvent()
	second["service"] = map[string]any{"name": "billing"}
	if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent(), second}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if srv.count() != 2 {
		t.Errorf("requests = %d, want 2, one per dataset", srv.count())
	}
}

// TestHoneycomb_Data proves a sample rate becomes a samplerate, an array becomes one
// JSON string, and a long string is cut.
func TestHoneycomb_Data(t *testing.T) {
	srv := newFake(t, http.StatusOK, `[{"status":202}]`)
	sender, err := honeycomb.NewSender(
		honeycomb.WithAPIKey("key"),
		honeycomb.WithAPIURL(srv.URL),
		honeycomb.WithDataset("logs"),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	event := requestEvent()
	event["wlog"] = map[string]any{"sample_rate": 0.5}
	event["tags"] = []any{"a", "b"}
	event["long"] = strings.Repeat("x", 70_000)
	for i := 0; i < 2100; i++ {
		event[fmt.Sprintf("z%04d", i)] = i
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	body, _ := srv.last()
	if !strings.Contains(body, `"samplerate":200`) {
		t.Error("the sample rate did not become a samplerate")
	}
	if !strings.Contains(body, `"tags":"[\"a\",\"b\"]"`) {
		t.Error("the array did not become one JSON string")
	}
	if strings.Contains(body, strings.Repeat("x", 70_000)) {
		t.Error("the long string was not cut")
	}
	var items []struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(items[0].Data) > 2000 {
		t.Errorf("data holds %d fields, want at most 2000", len(items[0].Data))
	}
}

// TestHoneycomb_BadURL proves a URL with no host is refused at New.
func TestHoneycomb_BadURL(t *testing.T) {
	if _, err := honeycomb.NewSender(honeycomb.WithAPIKey("key"), honeycomb.WithAPIURL("http://")); err == nil {
		t.Error("NewSender accepted a URL with no host")
	}
}
func TestHoneycomb_WithSpans(t *testing.T) {
	srv := newFake(t, http.StatusOK, `[{"status":202}]`)
	sender, err := honeycomb.NewSender(
		honeycomb.WithAPIKey("key"),
		honeycomb.WithAPIURL(srv.URL),
		honeycomb.WithDataset("logs"),
		honeycomb.WithSpans(false),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	event := requestEvent()
	event["trace"].(map[string]any)["parent_span_id"] = "1111111111111111"
	if err := sender.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	body, _ := srv.last()
	if strings.Contains(body, "trace.span_id") || strings.Contains(body, "trace.parent_id") {
		t.Errorf("WithSpans(false) kept a span key: %s", body)
	}

	on := newFake(t, http.StatusOK, `[{"status":202}]`)
	withSpans, err := honeycomb.NewSender(
		honeycomb.WithAPIKey("key"),
		honeycomb.WithAPIURL(on.URL),
		honeycomb.WithDataset("logs"),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	event["outcome"] = "error"
	if err := withSpans.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	body, _ = on.last()
	for _, want := range []string{`"name":"GET /orders"`, `"error":true`, `"trace.parent_id":"1111111111111111"`} {
		if !strings.Contains(body, want) {
			t.Errorf("WithSpans(true) body %s is missing %s", body, want)
		}
	}
}
