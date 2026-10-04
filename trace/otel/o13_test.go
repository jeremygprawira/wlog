package wlogotel_test

import (
	"errors"
	"testing"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
)

// failStatsMeter fails only the observable counters the stats use.
type failStatsMeter struct{ noop.Meter }

func (failStatsMeter) Int64ObservableCounter(string, ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	return nil, errors.New("counter failed")
}

type failStatsProvider struct{ noop.MeterProvider }

func (failStatsProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return failStatsMeter{}
}

// TestOtel_O13_PluginReturnsStatsError proves Plugin returns a stats counter error.
func TestOtel_O13_PluginReturnsStatsError(t *testing.T) {
	_, err := wlogotel.Plugin(wlogotel.WithMeterProvider(failStatsProvider{}), wlogotel.WithMetrics(false))
	if err == nil {
		t.Fatal("Plugin hid the stats counter error")
	}
}
