// This file builds the metric side of the plugin: two duration histograms, the
// per-kind operation cap, and the observable counters that read the Logger's Stats.
package wlogotel

import (
	"context"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/jeremygprawira/wlog"
)

// standardMethods are the nine HTTP methods the semantic conventions name. Any other
// method records as _OTHER, so a scanner that sends junk cannot grow the label set.
var standardMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true, "DELETE": true,
	"CONNECT": true, "OPTIONS": true, "TRACE": true, "PATCH": true,
}

// Measure records one duration histogram for a finished event. Core calls it before
// sampling, so a sampler never changes a metric.
func (p *plugin) Measure(ctx context.Context, m wlog.Measure) {
	if !p.cfg.metrics {
		return
	}
	seconds := m.DurationMS / 1000
	if m.Kind == "request" {
		p.request.Record(ctx, seconds, metric.WithAttributes(requestAttributes(m)...))
		return
	}
	operation := p.operation(m.Kind, m.Operation)
	p.work.Record(ctx, seconds, metric.WithAttributes(workAttributes(m, operation)...))
}

// requestAttributes names one request for the http.server.request.duration histogram.
// A path, a user id, and a client address stay out, because each one is unbounded.
func requestAttributes(m wlog.Measure) []attribute.KeyValue {
	out := []attribute.KeyValue{attribute.String("http.request.method", methodOf(m.Method))}
	if m.Scheme != "" {
		out = append(out, attribute.String("url.scheme", m.Scheme))
	}
	if code, ok := statusCode(m.Status); ok {
		out = append(out, attribute.Int("http.response.status_code", code))
	}
	if m.Route != "" {
		out = append(out, attribute.String("http.route", m.Route))
	}
	if m.ErrorType != "" {
		out = append(out, attribute.String("error.type", m.ErrorType))
	}
	return out
}

// workAttributes names one unit for the wlog.work.duration histogram.
func workAttributes(m wlog.Measure, operation string) []attribute.KeyValue {
	out := []attribute.KeyValue{
		attribute.String("wlog.kind", m.Kind),
		attribute.String("wlog.operation", operation),
		attribute.String("wlog.outcome", m.Outcome),
	}
	if m.System != "" {
		out = append(out, attribute.String("wlog.system", m.System))
	}
	if m.ErrorType != "" {
		out = append(out, attribute.String("error.type", m.ErrorType))
	}
	return out
}

// methodOf normalizes an HTTP method for a metric label.
func methodOf(method string) string {
	if standardMethods[method] {
		return method
	}
	return "_OTHER"
}

// statusCode reads an HTTP status as a number.
func statusCode(text string) (int, bool) {
	code, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, false
	}
	return code, true
}

// operation returns the operation to record for a kind. Past the cap, a new operation
// records as _OTHER, and the plugin reports WLOG_CAP_REACHED once for that kind.
func (p *plugin) operation(kind, name string) string {
	p.mu.Lock()
	seen := p.ops[kind]
	if seen == nil {
		seen = map[string]struct{}{}
		p.ops[kind] = seen
	}
	if _, ok := seen[name]; ok {
		p.mu.Unlock()
		return name
	}
	if len(seen) >= p.cfg.maxOps {
		first := !p.capped[kind]
		p.capped[kind] = true
		p.mu.Unlock()
		if first {
			p.reportCap(kind)
		}
		return "_OTHER"
	}
	seen[name] = struct{}{}
	p.mu.Unlock()
	return name
}

// reportCap tells the caller that one kind reached its operation cap.
func (p *plugin) reportCap(kind string) {
	if p.logger == nil {
		return
	}
	p.logger.Report(wlog.Problem{
		Code:    "WLOG_CAP_REACHED",
		Source:  "trace-otel",
		Message: "the operation cap was reached for kind " + kind,
	})
}

// registerStats registers the observable counters that read the Logger's Stats on every
// scrape, so a scrape sees the numbers at that moment.
func (p *plugin) registerStats(l *wlog.Logger) error {
	meter := p.cfg.mp.Meter(instrumentationName)
	emitted, err := meter.Int64ObservableCounter("wlog.events.emitted")
	if err != nil {
		return err
	}
	dropped, err := meter.Int64ObservableCounter("wlog.events.dropped")
	if err != nil {
		return err
	}
	writerDropped, err := meter.Int64ObservableCounter("wlog.writer.dropped")
	if err != nil {
		return err
	}
	drainEvents, err := meter.Int64ObservableCounter("wlog.drain.events")
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		stats := l.Stats()
		o.ObserveInt64(emitted, stats.Emitted)
		for reason, count := range stats.Dropped {
			o.ObserveInt64(dropped, count, metric.WithAttributes(attribute.String("wlog.reason", reason)))
		}
		o.ObserveInt64(writerDropped, stats.WriterDropped)
		for _, d := range stats.Drains {
			o.ObserveInt64(drainEvents, d.Queued, drainOption(d.Name, "queued"))
			o.ObserveInt64(drainEvents, d.Sent, drainOption(d.Name, "sent"))
			o.ObserveInt64(drainEvents, d.Dropped, drainOption(d.Name, "dropped"))
			o.ObserveInt64(drainEvents, d.Retried, drainOption(d.Name, "retried"))
		}
		return nil
	}, emitted, dropped, writerDropped, drainEvents)
	return err
}

// drainOption names one drain and one of its states.
func drainOption(drain, state string) metric.ObserveOption {
	return metric.WithAttributes(
		attribute.String("wlog.drain", drain),
		attribute.String("wlog.state", state))
}
