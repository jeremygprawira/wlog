// This file checks the aggregation of matched events with hand-computed values.
package query_test

import (
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog/query"
)

// timed builds one event with a route and a duration.
func timed(route string, duration float64, stamp string) map[string]any {
	return map[string]any{
		"kind": "request", "operation": "GET " + route, "timestamp": stamp,
		"http": map[string]any{"route": route}, "duration_ms": duration,
	}
}

// TestQuery_AggregateCounts proves the count per group, sorted by count.
func TestQuery_AggregateCounts(t *testing.T) {
	events := []map[string]any{
		timed("/a", 10, "2026-09-16T08:00:00Z"),
		timed("/a", 20, "2026-09-16T08:00:01Z"),
		timed("/b", 30, "2026-09-16T08:00:02Z"),
	}
	buckets := query.Counts(events, "http.route")
	if len(buckets) != 2 || buckets[0].Key != "/a" || buckets[0].Count != 2 || buckets[1].Key != "/b" || buckets[1].Count != 1 {
		t.Errorf("counts = %+v, want /a 2 then /b 1", buckets)
	}
}

// TestQuery_AggregateStats proves the nearest-rank percentiles of one sample.
func TestQuery_AggregateStats(t *testing.T) {
	events := []map[string]any{
		timed("/a", 10, "2026-09-16T08:00:00Z"),
		timed("/a", 20, "2026-09-16T08:00:01Z"),
		timed("/a", 30, "2026-09-16T08:00:02Z"),
		timed("/a", 40, "2026-09-16T08:00:03Z"),
	}
	stats := query.Statistics(events, "duration_ms")
	for name, want := range map[string]float64{"p50": 20, "p95": 40, "p99": 40, "max": 40} {
		got := map[string]float64{"p50": stats.P50, "p95": stats.P95, "p99": stats.P99, "max": stats.Max}[name]
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	if stats.Count != 4 {
		t.Errorf("count = %d, want 4", stats.Count)
	}

	groups := query.GroupStats(events, "http.route", "duration_ms")
	if len(groups) != 1 || groups[0].Key != "/a" || groups[0].Stats.P50 != 20 {
		t.Errorf("group stats = %+v, want one /a group with p50 20", groups)
	}
}

// TestQuery_AggregateSizes proves the bytes per event and the monthly rate of one
// sample.
func TestQuery_AggregateSizes(t *testing.T) {
	events := []map[string]any{
		timed("/a", 10, "2026-09-16T08:00:00Z"),
		timed("/a", 20, "2026-09-16T08:00:01Z"),
	}
	body, _ := json.Marshal(events[0])
	rows := query.Sizes(events)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one row", rows)
	}
	row := rows[0]
	if row.Kind != "request" || row.Events != 2 || row.Bytes != int64(len(body))*2 {
		t.Errorf("row = %+v, want two request events of %d bytes", row, len(body))
	}
	if row.BytesPerEvent != float64(len(body)) {
		t.Errorf("bytes per event = %v, want %d", row.BytesPerEvent, len(body))
	}
	// Two events one second apart: 2 * bytes per second, over a 30 day month.
	wantMonthly := float64(len(body)) * 2 * 30 * 24 * 3600 / 1e9
	if row.GBPerMonth != wantMonthly {
		t.Errorf("GB per month = %v, want %v", row.GBPerMonth, wantMonthly)
	}

	// One event, and one moment: no span, so no rate.
	rows = query.Sizes(events[:1])
	if rows[0].GBPerMonth != 0 {
		t.Errorf("GB per month = %v, want 0 with no span", rows[0].GBPerMonth)
	}
}
