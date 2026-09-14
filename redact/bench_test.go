package redact_test

import (
	"fmt"
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

// buildBenchEvent returns a ~50-field, 3-level-deep event with 10 string values, per
// SPEC-redact.md's benchmark criterion.
func buildBenchEvent() map[string]any {
	event := make(map[string]any, 50)
	for i := 0; i < 10; i++ {
		event[fmt.Sprintf("str%d", i)] = fmt.Sprintf("some text value number %d for benchmarking", i)
	}
	for i := 0; i < 36; i++ {
		event[fmt.Sprintf("num%d", i)] = i
	}
	event["nested"] = map[string]any{
		"level2": map[string]any{
			"level3": map[string]any{"password": "secret", "name": "alice"},
		},
	}
	return event
}

func BenchmarkRedact_Apply(b *testing.B) {
	r := redact.Default()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		event := buildBenchEvent()
		b.StartTimer()
		r.Apply(event)
	}
}
