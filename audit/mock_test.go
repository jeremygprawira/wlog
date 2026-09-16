package audit_test

import (
	"context"
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
	ctx, end = wlog.Start(ctx, "op")
	end()
	rec.RequireNoAudit(t)
}
