package datadog_test

import (
	"context"
	"encoding/json"
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
func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...datadog.Option) *datadog.Drain {
	t.Helper()
	all := append([]datadog.Option{datadog.WithURL(srv.URL), datadog.WithAPIKey("api-key")}, opts...)
	d, err := datadog.New(all...)
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

	d, err := datadog.New(datadog.WithURL(srv.URL))
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
	if _, err := datadog.New(datadog.WithURL("http://example.invalid")); err == nil {
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
		json.NewDecoder(r.Body).Decode(&items)
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

	d, err := datadog.New(datadog.WithURL(srv.URL), datadog.WithAPIKey("k"))
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
