// Package wlogotel turns one wlog event into an OpenTelemetry span, its attributes, its
// status, one exception event, and two duration histograms. It serves a team that
// already runs OTel, and it adds no requirement to wlog itself.
//
// The plugin is a Starter, a Finisher, and a Measurer. The Starter copies the active
// span's ids onto the event, so head sampling and every outbound call use the OTel
// trace. The Finisher copies the redacted event onto the span after finalize. The
// Measurer records a duration before sampling, so a sampler never changes a metric.
//
// The OTel middleware must wrap outside wlog, so the span exists before the unit
// starts. Register it first, then wlog:
//
//	// net/http
//	handler := otelhttp.NewHandler(wlogNetHTTP.Setup(mux), "server")
//
//	// chi
//	r.Use(otelhttp.NewMiddleware("server"))
//	r.Use(wlogchi.Middleware())
//
//	// gin
//	r.Use(otelgin.Middleware("server"))
//	r.Use(wloggin.Middleware())
//
//	// echo
//	e.Use(otelecho.Middleware("server"))
//	e.Use(wlogecho.Middleware())
//
//	// gRPC
//	grpc.NewServer(
//		grpc.ChainUnaryInterceptor(otelgrpc.UnaryServerInterceptor(), wloggrpc.UnaryServerInterceptor()),
//	)
//
// The plugin never creates a span. With no recording span on the unit context it does
// nothing, so an app without OTel pays almost nothing.
package wlogotel

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// instrumentationName identifies this package to OTel.
const instrumentationName = "github.com/jeremygprawira/wlog"

// semconvBuckets are the histogram buckets of the OTel semantic conventions for a
// duration in seconds.
var semconvBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// config holds the resolved options of one plugin.
type config struct {
	spans     bool
	metrics   bool
	stats     bool
	exception bool
	maxOps    int
	mp        metric.MeterProvider
}

// defaultConfig returns the defaults the spec names.
func defaultConfig() config {
	return config{spans: true, metrics: true, stats: true, exception: true, maxOps: 1000}
}

// Option configures Plugin.
type Option func(*config)

// WithMeterProvider sets the meter provider. The default is the global one.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *config) {
		if mp != nil {
			c.mp = mp
		}
	}
}

// WithSpans turns the span attributes, the status, and the exception event on or off.
// The default is on.
func WithSpans(on bool) Option { return func(c *config) { c.spans = on } }

// WithMetrics turns the two duration histograms on or off. The default is on.
func WithMetrics(on bool) Option { return func(c *config) { c.metrics = on } }

// WithStats turns the observable counters that read the Logger's Stats on or off. The
// default is on.
func WithStats(on bool) Option { return func(c *config) { c.stats = on } }

// WithExceptionEvent turns the one span event named exception on or off. Turn it off
// when trace-otellog also runs, so OTel records each exception once. The default is on.
func WithExceptionEvent(on bool) Option { return func(c *config) { c.exception = on } }

// WithMaxOperations caps how many distinct operations one kind records. A later
// operation records as _OTHER. The default is 1000 per kind.
func WithMaxOperations(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxOps = n
		}
	}
}

// plugin holds the instruments and the per-kind operation cap of one Logger.
type plugin struct {
	cfg    config
	tracer oteltrace.Tracer

	request metric.Float64Histogram
	work    metric.Float64Histogram

	mu     sync.Mutex
	ops    map[string]map[string]struct{}
	capped map[string]bool

	logger *wlog.Logger
}

// Plugin returns a wlog.Plugin that fills trace ids, spans, metrics, and stats. It
// returns an error when an instrument fails to build. Nothing panics.
func Plugin(opts ...Option) (wlog.Plugin, error) {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	if c.mp == nil {
		c.mp = otel.GetMeterProvider()
	}
	p := &plugin{
		cfg:    c,
		ops:    map[string]map[string]struct{}{},
		capped: map[string]bool{},
	}
	if c.spans {
		p.tracer = otel.GetTracerProvider().Tracer(instrumentationName)
	}
	if c.metrics {
		meter := c.mp.Meter(instrumentationName)
		var err error
		p.request, err = meter.Float64Histogram("http.server.request.duration",
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(semconvBuckets...))
		if err != nil {
			return nil, err
		}
		p.work, err = meter.Float64Histogram("wlog.work.duration",
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(semconvBuckets...))
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Name returns the plugin name, which OnProblem reports as the source.
func (p *plugin) Name() string { return "trace-otel" }

// Setup keeps the Logger, so the operation cap and the stats counters can report
// through it, and it registers the observable counters.
func (p *plugin) Setup(l *wlog.Logger) error {
	p.logger = l
	if !p.cfg.stats || !p.cfg.metrics || l == nil {
		return nil
	}
	return p.registerStats(l)
}

// OnStart copies the ids of the active span onto the event, so head sampling and every
// outbound call use the OTel trace. With no recording span it leaves the context alone.
func (p *plugin) OnStart(ctx context.Context, _ string) context.Context {
	if !p.cfg.spans {
		return ctx
	}
	span := oteltrace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return ctx
	}
	sc := span.SpanContext()
	return propagate.ContextWith(ctx, propagate.TraceContext{
		TraceID: sc.TraceID().String(),
		SpanID:  sc.SpanID().String(),
		Sampled: sc.IsSampled(),
	})
}
