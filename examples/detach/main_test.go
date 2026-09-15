package main

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestDetachExample_ChildOutlivesParent proves sendEmail emits its own event and keeps
// the parent operation as trace.parent_operation, instead of joining the parent event.
func TestDetachExample_ChildOutlivesParent(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "checkout")
	sendEmail(ctx)
	end()

	byOperation := map[string]map[string]any{}
	for _, event := range rec.Events() {
		byOperation[event["operation"].(string)] = event
	}
	child := byOperation["email.send"]
	if child == nil {
		t.Fatalf("no email.send event; got %v", rec.Events())
	}
	if child["email.recipient_id"] != "u-42" {
		t.Errorf("email.recipient_id = %v, want u-42", child["email.recipient_id"])
	}
	trace, _ := child["trace"].(map[string]any)
	if trace["parent_operation"] != "checkout" {
		t.Errorf("trace.parent_operation = %v, want checkout", trace["parent_operation"])
	}
}
