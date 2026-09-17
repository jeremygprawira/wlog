package audit_test

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
)

// TestAudit_Mock proves the mock logger records audit facts, and each assertion helper
// reads the last one.
func TestAudit_Mock(t *testing.T) {
	log, rec := audit.Mock(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	audit.Do(ctx, audit.Record{
		Actor:   audit.Actor{Type: "user", ID: "u-1"},
		Action:  "invoice.refund",
		Outcome: "success",
	})
	end()

	rec.RequireAction(t, "invoice.refund")
	rec.RequireOutcome(t, "success")
	rec.RequireActor(t, "user", "u-1")

	ctx = log.WithContext(context.Background())
	_, end = wlog.Start(ctx, "op")
	end()
	rec.RequireNoAudit(t)
}

// TestAudit_AUD12_MockIgnoresEnv proves the mock writes nothing at all, and that
// WLOG_LEVEL in the environment cannot hide a record from a test.
func TestAudit_AUD12_MockIgnoresEnv(t *testing.T) {
	t.Setenv("WLOG_LEVEL", "error")
	t.Setenv("WLOG_FORMAT", "pretty")

	// Capture stdout before Mock builds the Logger, because Mock must write no line.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	log, rec := audit.Mock(t)
	ctx := log.WithContext(context.Background())
	audit.Do(ctx, audit.Record{Action: "invoice.refund", Outcome: "success"})
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if len(out) != 0 {
		t.Errorf("Mock wrote %q to stdout, want silence", out)
	}

	rec.RequireAction(t, "invoice.refund")
}
