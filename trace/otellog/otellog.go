// Package wlogotellog sends one wlog event to the OpenTelemetry Logs Bridge as one log
// record, so an app that already runs OTel gets every event in its existing pipeline.
//
// The app owns the provider, so it owns batching, export, and the resource. New takes a
// log.LoggerProvider, or nil for the global one:
//
//	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(exporter))
//	log := wlog.New(wlog.WithDrains(wlogotellog.New(provider)))
//
// The record takes its timestamp from the event, its severity from the level, its body
// from the summary, and its name from the kind, as in wlog.request. Attributes come from
// the otel output preset, so a span and a log record name the same field the same way.
//
// A record links to its trace even when the context holds no span, because Send builds a
// span context from the event's trace ids.
//
// The otel/log API is v0 and can change in a minor release. This package pins the
// version it was written against.
package wlogotellog

import (
	"context"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jeremygprawira/wlog"
)

// instrumentationName identifies this package to OTel.
const instrumentationName = "github.com/jeremygprawira/wlog"

// drain turns one event into one log record.
type drain struct {
	logger   log.Logger
	service  bool
	problems *wlog.Logger
}

// config holds the resolved options of one drain.
type config struct {
	serviceAttributes bool
}

// Option configures New.
type Option func(*config)

// WithServiceAttributes adds the resource keys, such as service.name, to each record.
// Use it when the provider resource holds no service identity. The default is false.
func WithServiceAttributes(on bool) Option {
	return func(c *config) { c.serviceAttributes = on }
}

// New returns a wlog.Drain that emits one log record per event through provider. A nil
// provider uses the global one. The app's provider owns batching, export, and the
// resource.
func New(provider log.LoggerProvider, opts ...Option) wlog.Drain {
	c := config{}
	for _, o := range opts {
		o(&c)
	}
	if provider == nil {
		provider = global.GetLoggerProvider()
	}
	return &drain{
		logger:  provider.Logger(instrumentationName, log.WithInstrumentationVersion(version())),
		service: c.serviceAttributes,
	}
}

// Setup keeps the Logger, so the drain can report through Logger.Report.
func (d *drain) Setup(l *wlog.Logger) error {
	d.problems = l
	return nil
}

// Send maps the redacted event to one log record and emits it. A record the provider
// disables is skipped, so a disabled severity costs nothing.
func (d *drain) Send(ctx context.Context, event map[string]any) {
	level, _ := event["level"].(string)
	kind, _ := event["kind"].(string)
	name := "wlog." + kind
	severity, text := severityOf(level)

	if !d.logger.Enabled(ctx, log.EnabledParameters{Severity: severity, EventName: name}) {
		return
	}

	var record log.Record
	record.SetEventName(name)
	record.SetSeverity(severity)
	record.SetSeverityText(text)
	record.SetObservedTimestamp(time.Now())
	if stamp, ok := timestampOf(event); ok {
		record.SetTimestamp(stamp)
	}
	if summary, ok := event["summary"].(string); ok {
		record.SetBody(attribute.StringValue(summary))
	}
	record.AddAttributes(recordAttributes(event, d.service)...)

	d.logger.Emit(withEventSpan(ctx, event), record)
}

// severityOf maps a wlog level to an OTel severity and its text.
func severityOf(level string) (log.Severity, string) {
	switch wlog.Level(level) {
	case wlog.LevelDebug:
		return log.SeverityDebug, log.SeverityDebug.String()
	case wlog.LevelWarn:
		return log.SeverityWarn, log.SeverityWarn.String()
	case wlog.LevelError:
		return log.SeverityError, log.SeverityError.String()
	default:
		return log.SeverityInfo, log.SeverityInfo.String()
	}
}

// timestampOf reads the event timestamp, which the canonical event holds as RFC 3339.
func timestampOf(event map[string]any) (time.Time, bool) {
	switch value := event["timestamp"].(type) {
	case time.Time:
		return value, true
	case string:
		stamp, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, false
		}
		return stamp, true
	}
	return time.Time{}, false
}

// withEventSpan puts the event's trace ids on the context when it holds no valid span,
// so a record from a propagate-derived trace still links to it.
func withEventSpan(ctx context.Context, event map[string]any) context.Context {
	if oteltrace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}
	group, _ := event["trace"].(map[string]any)
	traceID, err := oteltrace.TraceIDFromHex(textOf(group["trace_id"]))
	if err != nil {
		return ctx
	}
	spanID, err := oteltrace.SpanIDFromHex(textOf(group["span_id"]))
	if err != nil {
		return ctx
	}
	span := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{TraceID: traceID, SpanID: spanID})
	return oteltrace.ContextWithSpanContext(ctx, span)
}

// version returns the wlog module version from the build info, or dev.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/jeremygprawira/wlog" {
			return dep.Version
		}
	}
	return "dev"
}
