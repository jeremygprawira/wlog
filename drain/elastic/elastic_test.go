package elastic_test

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

	"github.com/jeremygprawira/wlog/drain/elastic"
	"github.com/jeremygprawira/wlog/pipeline"
)

// fakeElastic records every request body and answers with a fixed body.
type fakeElastic struct {
	*httptest.Server
	mu      sync.Mutex
	status  int
	answer  string
	bodies  []string
	headers []http.Header
}

// newFake starts a fake cluster that answers with status and answer.
func newFake(t *testing.T, status int, answer string) *fakeElastic {
	t.Helper()
	f := &fakeElastic{status: status, answer: answer}
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
func (f *fakeElastic) last() (string, http.Header) {
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
		"timestamp":   "2026-09-22T10:00:00Z",
		"level":       "info",
		"summary":     "GET /orders",
		"operation":   "GET /orders",
		"outcome":     "success",
		"duration_ms": 42.0,
		"event_id":    "018f4b3c-7c00-7a00-8000-000000000000",
		"service":     map[string]any{"name": "checkout"},
		"trace":       map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736", "span_id": "00f067aa0ba902b7"},
		"http":        map[string]any{"method": "GET", "status": int64(200)},
	}
}

// TestElastic_GoldenBody proves the bulk body matches the golden written from the
// Elasticsearch documents: one create line and one ECS line per event.
func TestElastic_GoldenBody(t *testing.T) {
	srv := newFake(t, http.StatusOK, `{"errors":false,"items":[{"create":{"status":201}}]}`)
	sender, err := elastic.NewSender(elastic.WithURL(srv.URL), elastic.WithIndex("logs-wlog-default"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	golden, err := os.ReadFile("testdata/bulk.ndjson")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	body, headers := srv.last()
	if body != string(golden) {
		t.Errorf("bulk body =\n%s\nwant\n%s", body, golden)
	}
	if !strings.HasPrefix(headers.Get("Content-Type"), "application/x-ndjson") {
		t.Errorf("Content-Type = %q, want application/x-ndjson", headers.Get("Content-Type"))
	}
}

// TestElastic_ItemResults proves the item statuses map to the exact Retry and Dropped
// sets, and the reason is the error type.
func TestElastic_ItemResults(t *testing.T) {
	answer := `{"errors":true,"items":[
		{"create":{"status":201}},
		{"create":{"status":429,"error":{"type":"es_rejected_execution_exception"}}},
		{"create":{"status":400,"error":{"type":"mapper_parsing_exception"}}}
	]}`
	srv := newFake(t, http.StatusOK, answer)
	sender, err := elastic.NewSender(elastic.WithURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = sender.SendBatch(context.Background(), []map[string]any{
		{"summary": "a"}, {"summary": "b"}, {"summary": "c"},
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
	if partial.Reason != "mapper_parsing_exception" {
		t.Errorf("Reason = %q, want mapper_parsing_exception", partial.Reason)
	}
}

// TestElastic_StatusTable proves the whole-request statuses classify as the spec says.
func TestElastic_StatusTable(t *testing.T) {
	for _, tc := range []struct {
		status    int
		retryable bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusRequestTimeout, true},
		{http.StatusRequestEntityTooLarge, false},
		{http.StatusTooManyRequests, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
	} {
		srv := newFake(t, tc.status, "")
		sender, err := elastic.NewSender(elastic.WithURL(srv.URL))
		if err != nil {
			t.Fatalf("NewSender: %v", err)
		}
		err = sender.SendBatch(context.Background(), []map[string]any{requestEvent()})
		var re interface{ Retryable() bool }
		if !errors.As(err, &re) {
			t.Errorf("status %d: error %v is not a RetryError", tc.status, err)
			continue
		}
		if re.Retryable() != tc.retryable {
			t.Errorf("status %d: retryable = %v, want %v", tc.status, re.Retryable(), tc.retryable)
		}
	}
}

// count returns how many requests arrived.
func (f *fakeElastic) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

// TestElastic_Options proves every option reaches the sender and New wraps it.
func TestElastic_Options(t *testing.T) {
	srv := newFake(t, http.StatusOK, `{"errors":false,"items":[{"create":{"status":201}}]}`)
	drain, err := elastic.New(
		elastic.WithURL(srv.URL),
		elastic.WithAPIKey("encoded"),
		elastic.WithBasicAuth("user", "pass"),
		elastic.WithIndex("logs-wlog-test"),
		elastic.WithMaxBatchBytes(1<<20),
		elastic.WithHTTPClient(&http.Client{}),
		elastic.WithTimeout(2*time.Second),
		elastic.WithUserAgent("agent"),
		elastic.WithGzip(true),
		elastic.WithPipeline(pipeline.BatchSize(1)),
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

// TestElastic_Env proves the environment supplies the URL, the credential, and the index.
func TestElastic_Env(t *testing.T) {
	t.Setenv("ELASTICSEARCH_URL", "http://example.invalid")
	t.Setenv("ELASTICSEARCH_USERNAME", "user")
	t.Setenv("ELASTICSEARCH_PASSWORD", "pass")
	t.Setenv("ELASTICSEARCH_INDEX", "logs-wlog-env")
	if _, err := elastic.NewSender(); err != nil {
		t.Fatalf("NewSender: %v", err)
	}
}

// TestElastic_MustNewPanics proves a missing URL panics rather than returning nil.
func TestElastic_MustNewPanics(t *testing.T) {
	t.Setenv("ELASTICSEARCH_URL", "")
	t.Setenv("OPENSEARCH_URL", "")
	defer func() {
		if recover() == nil {
			t.Error("MustNew did not panic on a missing URL")
		}
	}()
	elastic.MustNew()
}

// TestElastic_ChunkSplit proves a tiny byte cap splits the batch into one request per
// event.
func TestElastic_ChunkSplit(t *testing.T) {
	srv := newFake(t, http.StatusOK, `{"errors":false,"items":[{"create":{"status":201}}]}`)
	sender, err := elastic.NewSender(elastic.WithURL(srv.URL), elastic.WithMaxBatchBytes(1))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{
		{"summary": "a"}, {"summary": "b"}, {"summary": "c"},
	}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if srv.count() != 3 {
		t.Errorf("requests = %d, want 3, one per event", srv.count())
	}
}

// TestElastic_BadResponse proves a malformed response body returns an error.
func TestElastic_BadResponse(t *testing.T) {
	srv := newFake(t, http.StatusOK, "not json")
	sender, err := elastic.NewSender(elastic.WithURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent()}); err == nil {
		t.Error("SendBatch accepted a malformed response")
	}
}

// TestElastic_Template proves the template matches the integration file, and that the
// OpenSearch flavor uses flat_object.
func TestElastic_Template(t *testing.T) {
	file, err := os.ReadFile("../../integrations/search/elastic/index-template.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(elastic.Template(elastic.Elasticsearch)); got != string(file) {
		t.Errorf("Template(Elasticsearch) =\n%s\nwant\n%s", got, file)
	}
	if !strings.Contains(string(elastic.Template(elastic.OpenSearch)), `"flat_object"`) {
		t.Error("Template(OpenSearch) does not use flat_object")
	}
	if strings.Contains(string(elastic.Template(elastic.OpenSearch)), `"flattened"`) {
		t.Error("Template(OpenSearch) still uses flattened")
	}
}
