// This file holds the Prometheus collector that reads the Logger's Stats on each scrape,
// so a scrape sees the numbers at that moment.
package wlogprom

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/jeremygprawira/wlog"
)

// statsCollector reads one Logger's Stats on each scrape.
type statsCollector struct {
	logger        *wlog.Logger
	emitted       *prometheus.Desc
	dropped       *prometheus.Desc
	writerDropped *prometheus.Desc
	drainEvents   *prometheus.Desc
}

// StatsCollector returns a Prometheus collector that exports the counters of one Logger.
// It reads Stats on each scrape, so a scrape sees the numbers at that moment.
func StatsCollector(l *wlog.Logger) prometheus.Collector {
	return &statsCollector{
		logger:        l,
		emitted:       prometheus.NewDesc("wlog_events_emitted_total", "Events that reached the drains and the writers.", nil, nil),
		dropped:       prometheus.NewDesc("wlog_events_dropped_total", "Events that did not reach a sink, by reason.", []string{"reason"}, nil),
		writerDropped: prometheus.NewDesc("wlog_writer_dropped_total", "Lines the async writer queue dropped.", nil, nil),
		drainEvents:   prometheus.NewDesc("wlog_drain_events_total", "Events per drain and state.", []string{"drain", "state"}, nil),
	}
}

// Describe sends the descriptors of every counter the collector reports.
func (c *statsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.emitted
	ch <- c.dropped
	ch <- c.writerDropped
	ch <- c.drainEvents
}

// Collect sends one counter per number the Logger reports.
func (c *statsCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.logger.Stats()
	ch <- prometheus.MustNewConstMetric(c.emitted, prometheus.CounterValue, float64(stats.Emitted))
	for reason, count := range stats.Dropped {
		ch <- prometheus.MustNewConstMetric(c.dropped, prometheus.CounterValue, float64(count), reason)
	}
	ch <- prometheus.MustNewConstMetric(c.writerDropped, prometheus.CounterValue, float64(stats.WriterDropped))
	for _, d := range stats.Drains {
		ch <- prometheus.MustNewConstMetric(c.drainEvents, prometheus.CounterValue, float64(d.Queued), d.Name, "queued")
		ch <- prometheus.MustNewConstMetric(c.drainEvents, prometheus.CounterValue, float64(d.Sent), d.Name, "sent")
		ch <- prometheus.MustNewConstMetric(c.drainEvents, prometheus.CounterValue, float64(d.Dropped), d.Name, "dropped")
		ch <- prometheus.MustNewConstMetric(c.drainEvents, prometheus.CounterValue, float64(d.Retried), d.Name, "retried")
	}
}
