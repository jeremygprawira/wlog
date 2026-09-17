package datadog_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/datadog"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
)

// newTestDrain points a Drain at a fake intake.
func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...datadog.Option) *datadog.Sender {
	t.Helper()
	all := append([]datadog.Option{datadog.WithURL(srv.URL), datadog.WithAPIKey("api-key")}, opts...)
	d, err := datadog.NewSender(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// item is one Datadog log item.
type item struct {
	DDSource string `json:"ddsource"`
	Service  string `json:"service"`
	DDTags   string `json:"ddtags"`
	Level    string `json:"level"`
	Message  string `json:"message"`
}

// TestDatadog_SendBatch_JSONArray proves one batch becomes one JSON array with the
// source, service, and tags the intake expects.
func TestDatadog_SendBatch_JSONArray(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	event := map[string]any{
		"level":     "error",
		"operation": "payment.charge",
		"service":   map[string]any{"name": "orders", "version": "1.4.0", "env": "prod"},
	}
	if err := d.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if got := req.Headers.Get("DD-API-KEY"); got != "api-key" {
		t.Errorf("DD-API-KEY = %q, want api-key", got)
	}
	if got := req.Headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := req.Headers.Get("X-Wlog-Source"); got != "datadog" {
		t.Errorf("X-Wlog-Source = %q, want datadog", got)
	}

	var items []item
	if err := json.Unmarshal(req.Body, &items); err != nil {
		t.Fatalf("body is not a JSON array: %v\n%s", err, req.Body)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].DDSource != "wlog" || items[0].Service != "orders" || items[0].Level != "error" {
		t.Errorf("item = %+v, want source wlog, service orders, level error", items[0])
	}
	if items[0].DDTags != "env:prod,service:orders,version:1.4.0" {
		t.Errorf("ddtags = %q", items[0].DDTags)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal([]byte(items[0].Message), &roundTrip); err != nil {
		t.Fatalf("message is not the event JSON: %v", err)
	}
	if roundTrip["operation"] != "payment.charge" {
		t.Errorf("message event = %v, want the whole event", roundTrip)
	}
}

// TestDatadog_EnvAPIKey proves DD_API_KEY configures the drain when no option is given.
func TestDatadog_EnvAPIKey(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("DD_API_KEY", "env-key")

	d, err := datadog.NewSender(datadog.WithURL(srv.URL))
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Headers.Get("DD-API-KEY"); got != "env-key" {
		t.Errorf("DD-API-KEY = %q, want env-key", got)
	}
}

// TestDatadog_MissingAPIKey proves a missing key is a construction error.
func TestDatadog_MissingAPIKey(t *testing.T) {
	if _, err := datadog.NewSender(datadog.WithURL("http://example.invalid")); err == nil {
		t.Fatal("New with no API key returned nil error")
	}
}

// TestDatadog_413SplitsBatch proves a payload-too-large response splits the batch in
// half until each request fits.
func TestDatadog_413SplitsBatch(t *testing.T) {
	var mu sync.Mutex
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []map[string]any
		_ = json.NewDecoder(r.Body).Decode(&items)
		mu.Lock()
		sizes = append(sizes, len(items))
		mu.Unlock()
		if len(items) > 2 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d, err := datadog.NewSender(datadog.WithURL(srv.URL), datadog.WithAPIKey("k"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	events := make([]map[string]any, 4)
	for i := range events {
		events[i] = map[string]any{"level": "info"}
	}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sizes) != 3 || sizes[0] != 4 || sizes[1] != 2 || sizes[2] != 2 {
		t.Errorf("request sizes = %v, want [4 2 2]", sizes)
	}
}

// TestDatadog_413SingleEventReturnsError proves one oversized event returns the error
// instead of looping forever.
func TestDatadog_413SingleEventReturnsError(t *testing.T) {
	srv := httpfake.New()
	srv.SetStatus(http.StatusRequestEntityTooLarge)
	defer srv.Close()
	d := newTestDrain(t, srv)

	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err == nil {
		t.Fatal("SendBatch returned nil error on a 413")
	}
}

// TestDatadog_NeverLeaksRedactedValue proves gate G1 end to end.
func TestDatadog_NeverLeaksRedactedValue(t *testing.T) {
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

// TestDatadog_PIPE15_SplitBatches proves a batch larger than the intake limits becomes
// several acceptable requests before it is sent, instead of one refused one.
func TestDatadog_PIPE15_SplitBatches(t *testing.T) {
	var mu sync.Mutex
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []map[string]any
		_ = json.NewDecoder(r.Body).Decode(&items)
		mu.Lock()
		sizes = append(sizes, len(items))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d, err := datadog.NewSender(datadog.WithURL(srv.URL), datadog.WithAPIKey("k"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	events := make([]map[string]any, 2500)
	for i := range events {
		events[i] = map[string]any{"level": "info", "operation": "op", "message": "m"}
	}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sizes) != 3 {
		t.Fatalf("the intake saw %d requests, want 3 (1000 + 1000 + 500): %v", len(sizes), sizes)
	}
	for i, size := range sizes {
		if size > 1000 {
			t.Errorf("request %d held %d items, over the intake's 1000-limit", i, size)
		}
	}
}

// TestDatadog_PIPE15_Retry413Half proves that after a 413 only the refused half is sent
// again, so a batch that partly succeeded is never repeated whole.
func TestDatadog_PIPE15_Retry413Half(t *testing.T) {
	var mu sync.Mutex
	var sizes []int
	first := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []map[string]any
		_ = json.NewDecoder(r.Body).Decode(&items)
		mu.Lock()
		sizes = append(sizes, len(items))
		tooLarge := first
		first = false
		mu.Unlock()
		if tooLarge {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d, err := datadog.NewSender(datadog.WithURL(srv.URL), datadog.WithAPIKey("k"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{
		{"level": "info", "n": 1},
		{"level": "info", "n": 2},
	}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []int{2, 1, 1}
	if len(sizes) != len(want) {
		t.Fatalf("the intake saw %v, want %v", sizes, want)
	}
	for i, size := range sizes {
		if size != want[i] {
			t.Fatalf("the intake saw %v, want %v", sizes, want)
		}
	}
}

// TestDatadog_PIPE25_EnvAlone proves the DD_* env vars configure everything, with no
// WithURL: DD_SITE names the site, and DD_SERVICE and DD_ENV are read once in New.
func TestDatadog_PIPE25_EnvAlone(t *testing.T) {
	t.Setenv("DD_API_KEY", "env-key")
	t.Setenv("DD_SITE", "example.test")
	t.Setenv("DD_SERVICE", "checkout")
	t.Setenv("DD_ENV", "prod")

	transport := &recordingTransport{}
	d, err := datadog.NewSender(datadog.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewSender from env: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	if len(transport.urls) != 1 {
		t.Fatalf("the client saw %v, want one request", transport.urls)
	}
	if want := "https://http-intake.logs.example.test/api/v2/logs"; transport.urls[0] != want {
		t.Errorf("URL = %q, want %q", transport.urls[0], want)
	}
	if got := transport.header.Get("DD-API-KEY"); got != "env-key" {
		t.Errorf("DD-API-KEY = %q, want env-key", got)
	}
	var items []item
	if err := json.Unmarshal(transport.body, &items); err != nil {
		t.Fatalf("body is not the intake array: %v", err)
	}
	if len(items) != 1 || items[0].Service != "checkout" {
		t.Errorf("service = %v, want DD_SERVICE's checkout", items)
	}
	if !strings.Contains(items[0].DDTags, "env:prod") {
		t.Errorf("ddtags = %q, want the DD_ENV tag", items[0].DDTags)
	}
}

// recordingTransport answers 200 and keeps the last request, so a test can assert on what
// a drain with no WithURL would have sent.
type recordingTransport struct {
	urls   []string
	body   []byte
	header http.Header
}

// RoundTrip records the request and returns an empty 200.
func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.urls = append(t.urls, req.URL.String())
	t.header = req.Header.Clone()
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		t.body = body
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
	}, nil
}
