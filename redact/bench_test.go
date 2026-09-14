package redact_test

import (
	"fmt"
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

// benchField is one flat entry of the benchmark event template, precomputed once so
// the timed loop never pays fmt.Sprintf's cost — only the map build and Apply itself.
type benchField struct {
	K string
	V any
}

var benchFields = buildBenchFields()

func buildBenchFields() []benchField {
	fields := make([]benchField, 0, 46)
	for i := 0; i < 10; i++ {
		fields = append(fields, benchField{
			fmt.Sprintf("str%d", i),
			fmt.Sprintf("some text value number %d for benchmarking", i),
		})
	}
	for i := 0; i < 36; i++ {
		fields = append(fields, benchField{fmt.Sprintf("num%d", i), i})
	}
	return fields
}

// buildBenchEvent returns a ~50-field, 3-level-deep event with 10 string values, per
// SPEC-redact.md's benchmark criterion.
func buildBenchEvent() map[string]any {
	event := make(map[string]any, len(benchFields)+1)
	for _, f := range benchFields {
		event[f.K] = f.V
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
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Apply(buildBenchEvent())
	}
}
