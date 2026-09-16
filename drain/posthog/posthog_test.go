package posthog_test

import (
	"context"
	"encoding/json"
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
	drain, err := posthog.New(posthog.WithAPIKey("key"), posthog.WithHost(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	event := map[string]any{
		"level": "error", "operation": "order.create",
		"user": map[string]any{"id": "u-1"},
		"http": map[string]any{"status": 500},
	}
	drain.Send(context.Background(), event)
	flush(t, drain)

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
	drain, _ := posthog.New(posthog.WithAPIKey("key"), posthog.WithHost(srv.URL))

	drain.Send(context.Background(), map[string]any{"trace": map[string]any{"request_id": "req-9"}})
	flush(t, drain)

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

	drain, err := posthog.New()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	drain.Send(context.Background(), map[string]any{"level": "info"})
	flush(t, drain)
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
