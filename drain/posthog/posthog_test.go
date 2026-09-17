package posthog_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/posthog"
	"github.com/jeremygprawira/wlog/internal/httpfake"
)

// flush closes a wrapped drain, which sends every buffered event.
func flush(t *testing.T, drain wlog.Drain) {
	t.Helper()
	closer, ok := drain.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("drain does not close")
	}
	if err := closer.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestPosthog_Batch proves one event becomes a batch entry with flattened properties and
// a distinct id from user.id.
func TestPosthog_Batch(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, err := posthog.NewSender(posthog.WithAPIKey("key"), posthog.WithHost(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	event := map[string]any{
		"level": "error", "operation": "order.create",
		"user": map[string]any{"id": "u-1"},
		"http": map[string]any{"status": 500},
	}
	if err := drain.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if req.Path != "/batch/" {
		t.Errorf("path = %q, want /batch/", req.Path)
	}
	var body struct {
		APIKey string `json:"api_key"`
		Batch  []struct {
			Event      string         `json:"event"`
			DistinctID string         `json:"distinct_id"`
			Properties map[string]any `json:"properties"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, req.Body)
	}
	if body.APIKey != "key" || len(body.Batch) != 1 {
		t.Fatalf("body = %+v", body)
	}
	entry := body.Batch[0]
	if entry.Event != "wlog_event" || entry.DistinctID != "u-1" {
		t.Errorf("entry = %+v, want wlog_event / u-1", entry)
	}
	if entry.Properties["http.status"] != float64(500) {
		t.Errorf("properties are not flattened: %v", entry.Properties)
	}
}

// TestPosthog_DistinctIDFallback proves trace.request_id is the second choice.
func TestPosthog_DistinctIDFallback(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, _ := posthog.NewSender(posthog.WithAPIKey("key"), posthog.WithHost(srv.URL))

	if err := drain.SendBatch(context.Background(), []map[string]any{map[string]any{"trace": map[string]any{"request_id": "req-9"}}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	if !strings.Contains(string(srv.Last().Body), `"distinct_id":"req-9"`) {
		t.Errorf("distinct_id not from trace.request_id: %s", srv.Last().Body)
	}
}

// TestPosthog_EnvAlone proves POSTHOG_API_KEY and POSTHOG_HOST configure the drain.
func TestPosthog_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("POSTHOG_API_KEY", "env-key")
	t.Setenv("POSTHOG_HOST", srv.URL)

	drain, err := posthog.NewSender()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := drain.SendBatch(context.Background(), []map[string]any{map[string]any{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if srv.Last() == nil {
		t.Fatal("no request reached the fake")
	}
}

// TestPosthog_NeverLeaksRedactedValue proves gate G1 end to end.
func TestPosthog_NeverLeaksRedactedValue(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, _ := posthog.New(posthog.WithAPIKey("key"), posthog.WithHost(srv.URL))

	log := wlog.New(wlog.WithDrains(drain))
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "password", "hunter2")
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(srv.Requests()) == 0 {
		t.Fatal("no request reached the fake")
	}
	for _, req := range srv.Requests() {
		if strings.Contains(string(req.Body), "hunter2") {
			t.Errorf("raw denied value reached the drain: %s", req.Body)
		}
	}
}

// TestPostHog_PIPE18_AnonymousEvent proves an event with no user id is marked as not a
// person, so PostHog does not bill one profile per request.
func TestPostHog_PIPE18_AnonymousEvent(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, err := posthog.NewSender(posthog.WithAPIKey("key"), posthog.WithHost(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := drain.SendBatch(context.Background(), []map[string]any{
		{"level": "info", "operation": "op", "trace": map[string]any{"request_id": "req-1"}},
	}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	var body struct {
		Batch []struct {
			Properties map[string]any `json:"properties"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(srv.Last().Body, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(body.Batch) != 1 {
		t.Fatalf("batch holds %d events, want 1", len(body.Batch))
	}
	if got, ok := body.Batch[0].Properties["$process_person_profile"]; !ok || got != false {
		t.Errorf("$process_person_profile = %v, want false for an event with no user id", got)
	}

	// An event with a user id is a person, so the marker is absent.
	if err := drain.SendBatch(context.Background(), []map[string]any{
		{"level": "info", "operation": "op", "user": map[string]any{"id": "u-1"}},
	}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	// Decode into a fresh value: json.Unmarshal reuses an existing map and would keep the
	// first response's keys.
	var withUser struct {
		Batch []struct {
			Properties map[string]any `json:"properties"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(srv.Last().Body, &withUser); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if _, marked := withUser.Batch[0].Properties["$process_person_profile"]; marked {
		t.Error("an event with a user id was marked as not a person")
	}
}

// TestPostHog_PIPE25_Golden proves the posted body matches the batch shape PostHog
// documents: an api_key and a batch array, each entry with event, distinct_id, timestamp,
// and properties. The golden file is written by hand from the vendor's documentation.
func TestPostHog_PIPE25_Golden(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d, err := posthog.NewSender(posthog.WithAPIKey("key"), posthog.WithHost(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	event := map[string]any{
		"timestamp": "2026-09-16T12:00:00Z",
		"level":     "error",
		"operation": "order.create",
		"user":      map[string]any{"id": "u-1"},
	}
	if err := d.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	want, err := os.ReadFile(filepath.Join("testdata", "batch.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got := prettyJSONPostHog(t, srv.Last().Body)
	if got != prettyJSONPostHog(t, want) {
		t.Errorf("body does not match the vendor shape\n--- got ---\n%s\n--- want ---\n%s", got, prettyJSONPostHog(t, want))
	}
}

// prettyJSONPostHog indents one JSON document, so a comparison ignores key order.
func prettyJSONPostHog(t *testing.T, body []byte) string {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, body)
	}
	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(pretty)
}
