package wlog_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// TestMeasure_O1_KindAndOperationFromFields proves a Measurer reads the kind and
// the operation an adapter wrote with Set, and that an unknown kind cannot become
// a metric label.
func TestMeasure_O1_KindAndOperationFromFields(t *testing.T) {
	p := &measurePlugin{}
	log := wlog.New(wlog.WithPlugins(p), wlog.WithHeadSampler(dropAll{}))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "GET unmatched")
	wlog.Set(ctx, "kind", "request")
	wlog.Set(ctx, "operation", "GET /orders/{id}")
	end()

	if len(p.measures) != 1 {
		t.Fatalf("measures = %d, want 1", len(p.measures))
	}
	got := p.measures[0]
	if got.Kind != "request" || got.Operation != "GET /orders/{id}" {
		t.Errorf("measure kind=%q operation=%q, want request and GET /orders/{id}", got.Kind, got.Operation)
	}

	p.measures = nil
	ctx, end = wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Set(ctx, "kind", "user-supplied")
	end()
	if len(p.measures) != 1 {
		t.Fatalf("capped measures = %d, want 1", len(p.measures))
	}
	if p.measures[0].Kind != "_OTHER" {
		t.Errorf("capped kind = %q, want _OTHER", p.measures[0].Kind)
	}
}
