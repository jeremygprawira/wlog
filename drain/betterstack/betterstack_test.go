package betterstack_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/betterstack"
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

// TestBetterStack_Array proves one batch becomes a JSON array with a Bearer header and
// the dt, level, and message columns mapped.
func TestBetterStack_Array(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, err := betterstack.New(betterstack.WithSourceToken("tok"), betterstack.WithHost(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	event := map[string]any{"timestamp": "2026-09-16T12:00:00Z", "level": "error", "operation": "job.run"}
	drain.Send(context.Background(), event)
	flush(t, drain)

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if got := req.Headers.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", got)
	}
	var items []map[string]any
	if err := json.Unmarshal(req.Body, &items); err != nil {
		t.Fatalf("body is not a JSON array: %v\n%s", err, req.Body)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0]["dt"] != "2026-09-16T12:00:00Z" || items[0]["level"] != "error" || items[0]["message"] != "job.run" {
		t.Errorf("item = %v, want dt/level/message mapped", items[0])
	}
}

// TestBetterStack_EnvAlone proves BETTERSTACK_SOURCE_TOKEN and BETTERSTACK_HOST configure
// the drain.
func TestBetterStack_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("BETTERSTACK_SOURCE_TOKEN", "env-tok")
	t.Setenv("BETTERSTACK_HOST", srv.URL)

	drain, err := betterstack.New()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	drain.Send(context.Background(), map[string]any{"level": "info"})
	flush(t, drain)
	if srv.Last() == nil {
		t.Fatal("no request reached the fake")
	}
}

// TestBetterStack_NeverLeaksRedactedValue proves gate G1 end to end.
func TestBetterStack_NeverLeaksRedactedValue(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	drain, _ := betterstack.New(betterstack.WithSourceToken("tok"), betterstack.WithHost(srv.URL))

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
