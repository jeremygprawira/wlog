// Package wlogprom records one duration histogram for every wlog event, so a Prometheus
// user gets rate, errors, and duration per operation. It reads no HTTP handler, because
// the app owns its registry and exposes it with promhttp.
//
// The recorder is a wlog.Plugin and a wlog.Measurer, so a sampler never changes a metric:
//
//	reg := prometheus.NewRegistry()
//	rec, err := wlogprom.New(reg)
//	if err != nil {
//		return err
//	}
//	log := wlog.New(wlog.WithPlugins(rec), wlog.WithDrains(...))
//	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
//
// One histogram, wlog_duration_seconds, holds every kind. Its labels are kind,
// operation, outcome, and status. A kind with no status records an empty status.
//
// After MaxOperations distinct operations in one kind, a new operation records as _OTHER,
// so a scanner that sends junk cannot grow the label set.
package wlogprom

import (
	"context"
	"errors"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jeremygprawira/wlog"
)

// histogramName is the one histogram this module records.
const histogramName = "wlog_duration_seconds"

// histogramHelp describes the histogram to a reader of the exposition.
const histogramHelp = "Duration of one wlog unit of work, in seconds."

// semconvBuckets are the histogram buckets of the OTel semantic conventions, so a
// Prometheus reader and an OTel reader see the same shape.
var semconvBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// histogramLabels are the label names of the histogram, in order.
var histogramLabels = []string{"kind", "operation", "outcome", "status"}

// config holds the resolved options of one recorder.
type config struct {
	buckets []float64
	maxOps  int
}

// defaultConfig returns the defaults the spec names.
func defaultConfig() config {
	return config{buckets: semconvBuckets, maxOps: 1000}
}

// Option configures New.
type Option func(*config)

// Buckets sets the histogram bucket bounds. The default is the semconv list.
func Buckets(b ...float64) Option {
	return func(c *config) {
		if len(b) > 0 {
			c.buckets = append([]float64(nil), b...)
		}
	}
}

// MaxOperations caps how many distinct operations one kind records. A later operation
// records as _OTHER. The default is 1000 per kind.
func MaxOperations(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxOps = n
		}
	}
}

// Recorder is a wlog.Plugin and a wlog.Measurer that records one histogram.
type Recorder struct {
	hist   *prometheus.HistogramVec
	shared *histState
	logger *wlog.Logger
}

// histState is the bucket list and the operation cap of one registered histogram.
// Every recorder that reuses the histogram shares this state.
type histState struct {
	buckets []float64
	maxOps  int
	mu      sync.Mutex
	ops     map[string]map[string]struct{}
	capped  map[string]bool
}

// histCollector is what New registers, so a second New can see the buckets and the cap.
type histCollector struct {
	hist  *prometheus.HistogramVec
	state *histState
}

// Describe forwards the histogram descriptors.
func (c *histCollector) Describe(ch chan<- *prometheus.Desc) { c.hist.Describe(ch) }

// Collect forwards the histogram samples.
func (c *histCollector) Collect(ch chan<- prometheus.Metric) { c.hist.Collect(ch) }

// New registers the histogram on reg and returns the recorder. If reg already holds the
// same histogram, New reuses it, so a second New on one registry works. Any other
// registration error returns an error.
func New(reg prometheus.Registerer, opts ...Option) (*Recorder, error) {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	if err := checkBuckets(c.buckets); err != nil {
		return nil, err
	}
	hist := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    histogramName,
		Help:    histogramHelp,
		Buckets: c.buckets,
	}, histogramLabels)
	col := &histCollector{hist: hist, state: &histState{
		buckets: append([]float64(nil), c.buckets...),
		maxOps:  c.maxOps,
		ops:     map[string]map[string]struct{}{},
		capped:  map[string]bool{},
	}}
	if err := reg.Register(col); err != nil {
		var already prometheus.AlreadyRegisteredError
		if !errors.As(err, &already) {
			return nil, err
		}
		existing, ok := already.ExistingCollector.(*histCollector)
		if !ok {
			return nil, err
		}
		if !sameBuckets(existing.state.buckets, c.buckets) || existing.state.maxOps != c.maxOps {
			return nil, errors.New("the registered histogram has different buckets or a different operation cap")
		}
		col = existing
	}
	return &Recorder{hist: col.hist, shared: col.state}, nil
}

// Name returns the plugin name, which OnProblem reports as the source.
func (r *Recorder) Name() string { return "metrics-prometheus" }

// Setup keeps the Logger, so the operation cap and a label error can report through it.
func (r *Recorder) Setup(l *wlog.Logger) error {
	r.logger = l
	return nil
}

// Measure records one observation for a finished event. Core calls it before sampling,
// so a sampler never changes a metric.
func (r *Recorder) Measure(_ context.Context, m wlog.Measure) {
	observer, err := r.hist.GetMetricWithLabelValues(m.Kind, r.operation(m.Kind, m.Operation), m.Outcome, m.Status)
	if err != nil {
		// A label error means the labels do not match the histogram, which is a fault in
		// this module rather than in the event. Report it and record nothing.
		r.report(err)
		return
	}
	observer.Observe(m.DurationMS / 1000)
}

// operation returns the operation to record for a kind. Past the cap, a new operation
// records as _OTHER, and the recorder reports WLOG_CAP_REACHED once for that kind.
func (r *Recorder) operation(kind, name string) string {
	r.shared.mu.Lock()
	seen := r.shared.ops[kind]
	if seen == nil {
		seen = map[string]struct{}{}
		r.shared.ops[kind] = seen
	}
	if _, ok := seen[name]; ok {
		r.shared.mu.Unlock()
		return name
	}
	if len(seen) >= r.shared.maxOps {
		first := !r.shared.capped[kind]
		r.shared.capped[kind] = true
		r.shared.mu.Unlock()
		if first {
			r.reportCap(kind)
		}
		return "_OTHER"
	}
	seen[name] = struct{}{}
	r.shared.mu.Unlock()
	return name
}

// sameBuckets reports whether two bucket lists are the same length and the same values.
func sameBuckets(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reportCap tells the caller that one kind reached its operation cap.
func (r *Recorder) reportCap(kind string) {
	if r.logger == nil {
		return
	}
	r.logger.Report(wlog.Problem{
		Code:    "WLOG_CAP_REACHED",
		Source:  "metrics-prometheus",
		Message: "the operation cap was reached for kind " + kind,
	})
}

// report tells the caller that the histogram refused a label set.
func (r *Recorder) report(err error) {
	if r.logger == nil {
		return
	}
	r.logger.Report(wlog.Problem{
		Code:    "WLOG_INVALID_CONFIG",
		Source:  "metrics-prometheus",
		Message: "the histogram refused a label set",
		Err:     err,
	})
}

// checkBuckets rejects bounds that are not strictly increasing. Prometheus panics
// on those bounds inside Observe, so New returns the error instead.
func checkBuckets(bounds []float64) error {
	for i := 1; i < len(bounds); i++ {
		if bounds[i] <= bounds[i-1] {
			return errors.New("histogram buckets must be strictly increasing")
		}
	}
	return nil
}
