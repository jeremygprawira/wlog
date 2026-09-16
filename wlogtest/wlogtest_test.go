package wlogtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/redact"
	"github.com/jeremygprawira/wlog/wlogtest"
)

func TestWlogtest_RequireField(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "order.create")
	wlog.Set(ctx, "order_id", "4821")
	end()

	rec.RequireField(t, "order_id", "4821")
	rec.RequireCount(t, 1)
}

func TestWlogtest_RequireErrorCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "order.get")
	wlog.Error(ctx, errors.New("boom"))
	end()

	rec.RequireErrorCode(t, "INTERNAL")
}

func TestWlogtest_UserOptionsApply(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithRedactor(redact.Disabled()))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "password", "hunter2")
	end()

	rec.RequireField(t, "password", "hunter2")
}

func TestWlogtest_LastAndEvents(t *testing.T) {
	log, rec := wlogtest.New(t)
	for _, op := range []string{"a", "b"} {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, op)
		end()
	}

	if len(rec.Events()) != 2 {
		t.Fatalf("Events() has %d entries, want 2", len(rec.Events()))
	}
	if rec.Last()["operation"] != "b" {
		t.Errorf("Last().operation = %v, want b", rec.Last()["operation"])
	}
}
