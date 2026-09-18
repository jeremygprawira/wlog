// This file builds the Measure of one finished event and hands it to every Measurer.
// A Measure holds reserved fields only, and every text field passes through the
// redactor's value patterns first, so a metric never carries a value a log line would
// hide (gate G1).
package wlog

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Measure holds the reserved fields of one finished event, for a metrics plugin. It
// never holds user keys, paths, bodies, or ids.
type Measure struct {
	Kind       string  // the event kind, never log
	Operation  string  // the name given to Start
	Level      Level   // the level the event ended at
	Outcome    string  // success, or error for a level of error
	DurationMS float64 // the time the work took, in milliseconds
	Status     string  // http.status, rpc.status_code, or cli.exit_code, as text
	Method     string  // http.method
	Route      string  // http.route
	Scheme     string  // http.scheme
	System     string  // rpc.system, messaging.system, job.system, or faas.system
	ErrorType  string  // error.code, else error.kind, else error.type
}

// Measurer receives one Measure for every finished event whose kind is not log. A
// metrics plugin, such as a Prometheus or an OpenTelemetry exporter, implements it.
type Measurer interface {
	Measure(ctx context.Context, m Measure)
}

// measureEvent calls every Measurer for one finished event. It runs before the level
// filter and before head sampling, so a filter or a sampler never changes a metric. A
// disabled or closed Logger calls no Measurer, and neither does a log line.
func (l *Logger) measureEvent(ctx context.Context, e *event, level Level) {
	if len(l.measurers) == 0 || e.kind == kindLog || !Enabled() {
		return
	}
	m := measureOf(e, level)
	l.redactMeasure(&m)
	for _, me := range l.measurers {
		l.measureOne(ctx, me, m)
	}
}

// measureOf builds the Measure of one event, reading reserved fields only.
func measureOf(e *event, level Level) Measure {
	return Measure{
		Kind:       e.kind,
		Operation:  e.operation,
		Level:      level,
		Outcome:    outcomeOf(level),
		DurationMS: float64(time.Since(e.start).Microseconds()) / 1000,
		Status:     textAt(e.fields, "http.status", "rpc.status_code", "cli.exit_code"),
		Method:     textAt(e.fields, "http.method"),
		Route:      textAt(e.fields, "http.route"),
		Scheme:     textAt(e.fields, "http.scheme"),
		System:     textAt(e.fields, "rpc.system", "messaging.system", "job.system", "faas.system"),
		ErrorType:  errorTypeOf(e.errInfo),
	}
}

// errorTypeOf names the error of an event for a metric: its code first, then its kind,
// then its Go type. The three live in ErrorInfo, which core holds beside the fields.
func errorTypeOf(info *ErrorInfo) string {
	switch {
	case info == nil:
		return ""
	case info.Code != "":
		return info.Code
	case info.Kind != "":
		return info.Kind
	default:
		return info.Type
	}
}

// textAt returns the first present path as text, and an empty string when no path is
// there. A path holds a group and a field, such as "http.status", and Cut keeps the
// lookup free of an allocation, because a Measurer runs on every event.
func textAt(event map[string]any, paths ...string) string {
	for _, path := range paths {
		group, field, _ := strings.Cut(path, ".")
		object, ok := event[group].(map[string]any)
		if !ok {
			continue
		}
		if value, ok := object[field]; ok {
			return fmt.Sprint(value)
		}
	}
	return ""
}

// measureTexts is the order of the text fields of one Measure: the key of each field,
// and a pointer to it, so the walk covers the fields that carry a value.
type measureTexts struct {
	key   string
	value *string
}

// redactMeasure masks the text fields of a Measure with the redactor's value patterns.
// One call covers every field that holds text, so a metric pays one walk and not one per
// field. An empty field costs nothing, and most events carry only one or two.
func (l *Logger) redactMeasure(m *Measure) {
	set := [...]measureTexts{
		{"operation", &m.Operation},
		{"status", &m.Status},
		{"method", &m.Method},
		{"route", &m.Route},
		{"scheme", &m.Scheme},
		{"system", &m.System},
		{"error_type", &m.ErrorType},
	}
	fields := make(map[string]any, len(set))
	for _, f := range set {
		if *f.value != "" {
			fields[f.key] = *f.value
		}
	}
	if len(fields) == 0 {
		return
	}
	l.currentRedactor().Apply(fields)
	for _, f := range set {
		if value, ok := fields[f.key]; ok {
			*f.value, _ = value.(string)
		}
	}
}

// measureOne runs one Measurer under recover. A panic reports WLOG_HOOK_PANIC and costs
// no other metric.
func (l *Logger) measureOne(ctx context.Context, me Measurer, m Measure) {
	defer func() {
		if r := recover(); r != nil {
			l.reportProblem(codeHookPanic, sourceName(me), fmt.Errorf("panic: %v", r))
		}
	}()
	me.Measure(ctx, m)
}
