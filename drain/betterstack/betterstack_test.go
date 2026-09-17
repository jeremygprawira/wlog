package betterstack_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/betterstack"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
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
	drain, err := betterstack.NewSender(betterstack.WithSourceToken("tok"), betterstack.WithHost(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	event := map[string]any{"timestamp": "2026-09-16T12:00:00Z", "level": "error", "operation": "job.run"}
	if err := drain.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

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

	drain, err := betterstack.NewSender()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := drain.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
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

// TestBetterStack_PIPE16_BareHost proves a bare host gets https, and that a value with no
// host at all is refused, so a typo fails at startup.
func TestBetterStack_PIPE16_BareHost(t *testing.T) {
	d, err := betterstack.NewSender(
		betterstack.WithSourceToken("tok"),
		betterstack.WithHost("in.logs.example.test"),
	)
	if err != nil {
		t.Fatalf("NewSender with a bare host: %v", err)
	}
	if d == nil {
		t.Fatal("NewSender returned no sender")
	}

	if _, err := betterstack.NewSender(betterstack.WithSourceToken("tok"), betterstack.WithHost("https://")); err == nil {
		t.Error("NewSender accepted a host with no host name")
	}

	// The current env name is read, and the older name stays an alias.
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("BETTERSTACK_SOURCE_TOKEN", "env-token")
	t.Setenv("BETTERSTACK_INGESTING_HOST", srv.URL)
	fromEnv, err := betterstack.NewSender()
	if err != nil {
		t.Fatalf("NewSender from env: %v", err)
	}
	if err := fromEnv.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Headers.Get("Authorization"); got != "Bearer env-token" {
		t.Errorf("Authorization = %q, want the env token", got)
	}
}

// TestBetterStack_PIPE25_Golden proves the posted body matches the array shape Better
// Stack documents: one object per event with dt, level, and message, plus the event's own
// fields. The golden file is written by hand from the vendor's documentation.
func TestBetterStack_PIPE25_Golden(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d, err := betterstack.NewSender(betterstack.WithSourceToken("tok"), betterstack.WithHost(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	event := map[string]any{
		"timestamp": "2026-09-16T12:00:00Z",
		"level":     "error",
		"operation": "payment.charge",
		"error":     map[string]any{"code": "PAYMENT_DECLINED", "message": "declined"},
		"service":   map[string]any{"name": "orders", "env": "prod"},
	}
	if err := d.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	want, err := os.ReadFile(filepath.Join("testdata", "logs.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got := prettyJSON(t, srv.Last().Body); got != prettyJSON(t, want) {
		t.Errorf("body does not match the vendor shape\n--- got ---\n%s\n--- want ---\n%s", got, prettyJSON(t, want))
	}
}

// prettyJSON indents one JSON document, so a comparison ignores key order.
func prettyJSON(t *testing.T, body []byte) string {
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

// TestBetterStack_New_WrapsWithPipelineDefaults proves New succeeds on a valid
// configuration, and rejects a missing token the same way NewSender does.
func TestBetterStack_New_WrapsWithPipelineDefaults(t *testing.T) {
	d, err := betterstack.New(betterstack.WithSourceToken("tok"), betterstack.WithHost("http://example.invalid"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	flush(t, d)

	t.Setenv("BETTERSTACK_SOURCE_TOKEN", "")
	if _, err := betterstack.New(); err == nil {
		t.Error("New with no token returned nil error")
	}
}

// TestBetterStack_MustNew_PanicsOnTheSameError proves MustNew is New plus a panic, not
// a different construction path.
func TestBetterStack_MustNew_PanicsOnTheSameError(t *testing.T) {
	t.Setenv("BETTERSTACK_SOURCE_TOKEN", "")
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustNew with no token did not panic")
			}
		}()
		betterstack.MustNew()
	}()

	d := betterstack.MustNew(betterstack.WithSourceToken("tok"), betterstack.WithHost("http://example.invalid"),
		betterstack.WithPipeline(pipeline.BatchSize(5)))
	flush(t, d)
}
