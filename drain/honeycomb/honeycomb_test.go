package honeycomb_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/honeycomb"
	"github.com/jeremygprawira/wlog/internal/httpfake"
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
		body := httpfake.Body(r)
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

// TestHoneycomb_P2_FailedDatasetLeavesTheOthersAlone proves that one dataset's failure
// does not resend the datasets that landed. The failed group goes to Retry or Dropped, and
// the loop continues, so the accepted datasets are sent once.
func TestHoneycomb_P2_FailedDatasetLeavesTheOthersAlone(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`[{"status":202}]`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	sender, err := honeycomb.NewSender(honeycomb.WithAPIKey("key"), honeycomb.WithAPIURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = sender.SendBatch(context.Background(), []map[string]any{
		{"service": map[string]any{"name": "checkout"}, "kind": "request"},
		{"service": map[string]any{"name": "billing"}, "kind": "request"},
	})
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2: one per dataset", requests)
	}
	if len(partial.Retry) != 1 || partial.Retry[0] != 1 {
		t.Errorf("Retry = %v, want the billing event at index 1", partial.Retry)
	}
	if len(partial.Dropped) != 0 {
		t.Errorf("Dropped = %v, want none for a 503", partial.Dropped)
	}
	if partial.Reason != "status_503" {
		t.Errorf("Reason = %q, want status_503", partial.Reason)
	}
}

// TestHoneycomb_P4_SplitsALargeRequestAndDropsAnOversizeEvent proves the two byte limits:
// an event whose item passes 1 MB is dropped with reason too_large, and a group whose body
// passes 5 MB arrives as several requests.
func TestHoneycomb_P4_SplitsALargeRequestAndDropsAnOversizeEvent(t *testing.T) {
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := httpfake.Body(r)
		var items []json.RawMessage
		_ = json.Unmarshal(body, &items)
		sizes = append(sizes, len(body))
		answers := make([]string, 0, len(items))
		for range items {
			answers = append(answers, `{"status":202}`)
		}
		_, _ = w.Write([]byte("[" + strings.Join(answers, ",") + "]"))
	}))
	defer srv.Close()

	sender, err := honeycomb.NewSender(honeycomb.WithAPIKey("key"), honeycomb.WithAPIURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	// The first event passes the 1 MB per-event limit. The rest pass the 5 MB per-request
	// limit together, so the sender must split them.
	big := strings.Repeat("x", 64*1024)
	oversize := map[string]any{"kind": "request", "service": map[string]any{"name": "logs"}}
	for i := 0; i < 20; i++ {
		oversize[fmt.Sprintf("field_%d", i)] = big
	}
	events := []map[string]any{oversize}
	for i := 0; i < 80; i++ {
		events = append(events, map[string]any{
			"kind":    "request",
			"service": map[string]any{"name": "logs"},
			"blob":    big + fmt.Sprint(i),
		})
	}

	err = sender.SendBatch(context.Background(), events)
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if len(partial.Dropped) != 1 || partial.Dropped[0] != 0 {
		t.Errorf("Dropped = %v, want the oversize event at index 0", partial.Dropped)
	}
	if partial.Reason != "too_large" {
		t.Errorf("Reason = %q, want too_large", partial.Reason)
	}
	if len(sizes) < 2 {
		t.Errorf("requests = %d, want more than one for a body over 5 MB", len(sizes))
	}
	for _, size := range sizes {
		if size > 5<<20 {
			t.Errorf("a request held %d bytes, want at most 5 MB", size)
		}
	}
}

// TestHoneycomb_P5_StatusTable proves the whole-request statuses classify as the spec says.
func TestHoneycomb_P5_StatusTable(t *testing.T) {
	for _, tc := range []struct {
		status    int
		retryable bool
		ok        bool
	}{
		{http.StatusAccepted, false, true},
		{http.StatusBadRequest, false, false},
		{http.StatusUnauthorized, false, false},
		{http.StatusForbidden, false, false},
		{http.StatusRequestTimeout, true, false},
		{http.StatusRequestEntityTooLarge, false, false},
		{http.StatusTooManyRequests, true, false},
		{http.StatusServiceUnavailable, true, false},
	} {
		srv := newFake(t, tc.status, `[{"status":202}]`)
		sender, err := honeycomb.NewSender(honeycomb.WithAPIKey("key"), honeycomb.WithAPIURL(srv.URL))
		if err != nil {
			t.Fatalf("NewSender: %v", err)
		}
		err = sender.SendBatch(context.Background(), []map[string]any{requestEvent()})
		if tc.ok {
			if err != nil {
				t.Errorf("status %d: SendBatch = %v, want nil", tc.status, err)
			}
			continue
		}
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

// TestHoneycomb_P6_GzipIsOnByDefault proves the drain compresses by default, and that
// WithGzip(false) turns it off.
func TestHoneycomb_P6_GzipIsOnByDefault(t *testing.T) {
	for _, tc := range []struct {
		name     string
		opts     []honeycomb.Option
		encoding string
	}{
		{"default", nil, "gzip"},
		{"off", []honeycomb.Option{honeycomb.WithGzip(false)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFake(t, http.StatusOK, `[{"status":202}]`)
			opts := append([]honeycomb.Option{
				honeycomb.WithAPIKey("key"), honeycomb.WithAPIURL(srv.URL),
			}, tc.opts...)
			sender, err := honeycomb.NewSender(opts...)
			if err != nil {
				t.Fatalf("NewSender: %v", err)
			}
			if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent()}); err != nil {
				t.Fatalf("SendBatch: %v", err)
			}
			_, headers := srv.last()
			if got := headers.Get("Content-Encoding"); got != tc.encoding {
				t.Errorf("Content-Encoding = %q, want %q", got, tc.encoding)
			}
		})
	}
}

// TestHoneycomb_P8_ShortResultListRetriesTheRest proves that an event without a result is
// retried, not counted as sent. A response of [], null, or {} names no event.
func TestHoneycomb_P8_ShortResultListRetriesTheRest(t *testing.T) {
	for _, answer := range []string{`[]`, `null`, `{}`} {
		srv := newFake(t, http.StatusOK, answer)
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
			t.Fatalf("answer %s: SendBatch = %v, want a PartialError", answer, err)
		}
		if len(partial.Retry) != 3 {
			t.Errorf("answer %s: Retry = %v, want all three events", answer, partial.Retry)
		}
		if len(partial.Dropped) != 0 {
			t.Errorf("answer %s: Dropped = %v, want none", answer, partial.Dropped)
		}
	}
}

// TestHoneycomb_P9_CapKeepsReservedKeys proves the field cap keeps the reserved keys, so a
// trace id and a service name survive a wide event.
func TestHoneycomb_P9_CapKeepsReservedKeys(t *testing.T) {
	srv := newFake(t, http.StatusOK, `[{"status":202}]`)
	sender, err := honeycomb.NewSender(
		honeycomb.WithAPIKey("key"),
		honeycomb.WithAPIURL(srv.URL),
		honeycomb.WithDataset("logs"),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	event := map[string]any{
		"level":     "info",
		"operation": "GET /orders",
		"kind":      "request",
		"service":   map[string]any{"name": "checkout"},
		"trace":     map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"},
	}
	for i := 0; i < 2100; i++ {
		event[fmt.Sprintf("a%04d", i)] = i
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	body, _ := srv.last()
	var items []struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	for _, key := range []string{"trace.trace_id", "service.name", "level", "operation"} {
		if _, ok := items[0].Data[key]; !ok {
			t.Errorf("the capped event lost %s", key)
		}
	}
}

// TestHoneycomb_P13_IndexMapAcrossDatasetsAndHalves proves the batch indexes stay right
// when several datasets are sent and one of them is halved after a 413. A billing event
// that the backend refuses must name its own batch position.
func TestHoneycomb_P13_IndexMapAcrossDatasetsAndHalves(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []map[string]any
		_ = json.Unmarshal(httpfake.Body(r), &items)
		if len(items) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		data, _ := items[0]["data"].(map[string]any)
		dataset, _ := data["service.name"].(string)
		if dataset == "checkout" && len(items) > 1 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if dataset == "checkout" {
			_, _ = w.Write([]byte(`[{"status":202}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"status":202},{"status":400}]`))
	}))
	defer srv.Close()

	sender, err := honeycomb.NewSender(honeycomb.WithAPIKey("key"), honeycomb.WithAPIURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	events := []map[string]any{
		{"kind": "request", "service": map[string]any{"name": "checkout"}},
		{"kind": "request", "service": map[string]any{"name": "checkout"}},
		{"kind": "request", "service": map[string]any{"name": "billing"}},
		{"kind": "request", "service": map[string]any{"name": "billing"}},
	}
	err = sender.SendBatch(context.Background(), events)
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if len(partial.Dropped) != 1 || partial.Dropped[0] != 3 {
		t.Errorf("Dropped = %v, want the billing event at batch index 3", partial.Dropped)
	}
	if len(partial.Retry) != 0 {
		t.Errorf("Retry = %v, want none", partial.Retry)
	}
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
