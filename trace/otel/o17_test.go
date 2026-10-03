package wlogotel_test

import (
	"os"
	"strings"
	"testing"
)

// TestOtel_O17_DropsDeadTracerAndPresetWalk proves the unused tracer and the
// error walk the preset already does are gone.
func TestOtel_O17_DropsDeadTracerAndPresetWalk(t *testing.T) {
	plugin, err := os.ReadFile("otel.go")
	if err != nil {
		t.Fatalf("ReadFile otel.go: %v", err)
	}
	if strings.Contains(string(plugin), "tracer oteltrace.Tracer") {
		t.Fatal("plugin.tracer is still declared and never read")
	}
	span, err := os.ReadFile("span.go")
	if err != nil {
		t.Fatalf("ReadFile span.go: %v", err)
	}
	if strings.Contains(string(span), `"error.code"`) {
		t.Fatal("spanErrorType still walks error.code, which the preset already maps")
	}
}
