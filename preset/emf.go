// This file holds the EMF preset: CloudWatch Embedded Metric Format, which Lambda and
// the CloudWatch agent read from stdout.
package preset

import (
	"fmt"
	"time"

	"github.com/jeremygprawira/wlog"
)

// Caps that EMF itself sets. A directive beyond a cap is dropped at construction.
const (
	emfMaxDimensionKeys = 30
	emfMaxMetrics       = 100
	emfMaxDimensionRune = 1024
)

// emfLead is the top-level order the EMF preset prints first.
var emfLead = []string{"_aws", "timestamp", "level", "summary"}

// emfDefaultMetrics are the metrics every request event reports.
var emfDefaultMetrics = []emfMetric{
	{name: "duration_ms", unit: "Milliseconds"},
	{name: "error_count", unit: "Count"},
}

// emfDefaultDimensions is the dimension set every request event reports.
var emfDefaultDimensions = [][]string{{"service.name", "operation"}}

// EMFOption configures an EMF preset.
type EMFOption func(*emfPreset)

// EMFNamespace sets the CloudWatch namespace. The default is wlog.
func EMFNamespace(ns string) EMFOption { return func(p *emfPreset) { p.namespace = ns } }

// EMFDimensions replaces the dimension sets. A set whose keys are not all present in an
// event is left out of that event.
func EMFDimensions(sets ...[]string) EMFOption {
	return func(p *emfPreset) { p.dimensions = sets }
}

// EMFMetrics adds numeric root keys to the metrics, each with the unit None.
func EMFMetrics(keys ...string) EMFOption {
	return func(p *emfPreset) {
		for _, key := range keys {
			p.metrics = append(p.metrics, emfMetric{name: key, unit: "None"})
		}
	}
}

// EMFLogGroup sets _aws.LogGroupName, which the CloudWatch agent reads.
func EMFLogGroup(name string) EMFOption { return func(p *emfPreset) { p.logGroup = name } }

// EMF returns the preset that writes CloudWatch Embedded Metric Format. A request event
// carries the _aws directive, and a log event carries none, because it has no duration.
func EMF(opts ...EMFOption) wlog.OutputPreset {
	p := emfPreset{
		namespace:  "wlog",
		dimensions: emfDefaultDimensions,
		metrics:    emfDefaultMetrics,
	}
	for _, opt := range opts {
		opt(&p)
	}
	p.cap()
	return p
}

// emfPreset writes one EMF line.
type emfPreset struct {
	namespace  string
	dimensions [][]string
	metrics    []emfMetric
	logGroup   string
	err        error
}

// emfMetric is one metric of the directive.
type emfMetric struct {
	name, unit string
}

// Name returns "emf".
func (emfPreset) Name() string { return "emf" }

// Lead returns the top-level keys the writer prints first.
func (emfPreset) Lead() []string { return emfLead }

// ConfigError returns the cap this preset had to trim, or nil.
func (p emfPreset) ConfigError() error { return p.err }

// Apply returns the event flattened to root keys, with the _aws directive for an event
// that carries a duration.
func (p emfPreset) Apply(event map[string]any) map[string]any {
	flat := Flat().Apply(event)
	if flat == nil {
		flat = map[string]any{}
	}
	if kind, _ := event["kind"].(string); kind == "log" {
		return flat
	}
	flat["error_count"] = emfErrorCount(event)
	p.dimensionValues(flat)
	flat["_aws"] = p.directive(flat)
	return flat
}

// cap drops a dimension set beyond 30 keys and a metric beyond 100, and records the trim
// so WithOutput reports WLOG_INVALID_CONFIG.
func (p *emfPreset) cap() {
	if len(p.metrics) > emfMaxMetrics {
		p.metrics = p.metrics[:emfMaxMetrics]
		p.note("EMF keeps at most %d metrics", emfMaxMetrics)
	}
	for i, set := range p.dimensions {
		if len(set) > emfMaxDimensionKeys {
			p.dimensions[i] = set[:emfMaxDimensionKeys]
			p.note("EMF keeps at most %d keys in a dimension set", emfMaxDimensionKeys)
		}
	}
}

// note records the first cap this preset hit.
func (p *emfPreset) note(format string, args ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(format, args...)
	}
}

// directive builds the _aws object: the event time, the metric directive, and the log
// group name.
func (p emfPreset) directive(flat map[string]any) map[string]any {
	aws := map[string]any{
		"Timestamp": emfMillis(flat["timestamp"]),
		"CloudWatchMetrics": []any{map[string]any{
			"Namespace":  p.namespace,
			"Dimensions": p.dimensionSets(flat),
			"Metrics":    p.metricList(),
		}},
	}
	if p.logGroup != "" {
		aws["LogGroupName"] = p.logGroup
	}
	return aws
}

// dimensionSets returns the dimension sets whose keys are all present, named by key.
// With no set left, it returns one empty set. EMF matches each name to the root key of
// the same name.
func (p emfPreset) dimensionSets(flat map[string]any) []any {
	sets := make([]any, 0, len(p.dimensions))
	for _, set := range p.dimensions {
		names := make([]any, 0, len(set))
		complete := true
		for _, key := range set {
			if _, present := flat[key]; !present {
				complete = false
				break
			}
			names = append(names, key)
		}
		if complete {
			sets = append(sets, names)
		}
	}
	if len(sets) == 0 {
		return []any{[]any{}}
	}
	return sets
}

// dimensionValues renders every key that a dimension set names as a string, cut to the
// EMF limit, so the root key holds the value the dimension reads.
func (p emfPreset) dimensionValues(flat map[string]any) {
	for _, set := range p.dimensions {
		for _, key := range set {
			if value, present := flat[key]; present {
				flat[key] = emfDimension(value)
			}
		}
	}
}

// metricList returns the metrics of the directive.
func (p emfPreset) metricList() []any {
	metrics := make([]any, 0, len(p.metrics))
	for _, metric := range p.metrics {
		metrics = append(metrics, map[string]any{"Name": metric.name, "Unit": metric.unit})
	}
	return metrics
}

// emfDimension renders one dimension value as a string, cut to the EMF limit.
func emfDimension(value any) string {
	text := textOf(value)
	runes := []rune(text)
	if len(runes) > emfMaxDimensionRune {
		return string(runes[:emfMaxDimensionRune])
	}
	return text
}

// emfErrorCount is 1 for an event that failed, and 0 otherwise.
func emfErrorCount(event map[string]any) int {
	if outcome, _ := event["outcome"].(string); outcome == "error" {
		return 1
	}
	return 0
}

// emfMillis renders the event time as epoch milliseconds.
func emfMillis(value any) int64 {
	stamp, err := time.Parse(time.RFC3339Nano, stringOf(value))
	if err != nil {
		return 0
	}
	return stamp.UnixMilli()
}
