package wlogotellog_test

import (
	"context"
	"testing"

	wlogotellog "github.com/jeremygprawira/wlog/trace/otellog"
)

// TestOtellog_O15_SeverityTextIsLevel proves the severity text is the wlog level.
func TestOtellog_O15_SeverityTextIsLevel(t *testing.T) {
	exporter := &captureExporter{}
	drain := wlogotellog.New(providerFor(exporter))

	drain.Send(context.Background(), errorRequest())

	got := exporter.snapshot()[0].SeverityText()
	if got != "error" {
		t.Fatalf("severity text = %q, want the wlog level %q", got, "error")
	}
}
