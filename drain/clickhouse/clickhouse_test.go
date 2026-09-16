package clickhouse_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/clickhouse"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
)

// newTestDrain points a Drain at a fake ClickHouse HTTP endpoint.
func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...clickhouse.Option) *clickhouse.Drain {
	t.Helper()
	all := append([]clickhouse.Option{
		clickhouse.WithURL(srv.URL),
		clickhouse.WithBasicAuth("default", "pass"),
		clickhouse.WithDatabase("analytics"),
		clickhouse.WithTable("events"),
	}, opts...)
	d, err := clickhouse.New(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// fullEvent builds an event with every mapped field set.
func fullEvent() map[string]any {
	return map[string]any{
		"timestamp":   "2026-09-16T12:00:00Z",
		"level":       "error",
		"operation":   "payment.charge",
		"duration_ms": 42,
		"outcome":     "error",
		"service":     map[string]any{"name": "orders", "version": "1.4.0", "env": "prod"},
		"trace":       map[string]any{"trace_id": "4bf9", "span_id": "00f0", "request_id": "req-1"},
		"http":        map[string]any{"method": "POST", "route": "/pay/:id", "status": 500},
		"error":       map[string]any{"code": "PAYMENT_DECLINED", "message": "declined"},
	}
}

// TestClickHouse_SendBatch_JSONEachRow proves one batch becomes one INSERT request with
// JSONEachRow lines and the reserved columns mapped.
func TestClickHouse_SendBatch_JSONEachRow(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	if err := d.SendBatch(context.Background(), []map[string]any{fullEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if req.Method != "POST" {
		t.Errorf("method = %s, want POST", req.Method)
	}
	query := req.Query.Get("query")
	if query != "INSERT INTO analytics.events FORMAT JSONEachRow" {
		t.Errorf("query = %q", query)
	}
	if strings.Contains(query, "CREATE") {
		t.Errorf("the drain tried to create a table: %q", query)
	}
	if got := req.Headers.Get("Content-Type"); got != "application/x-ndjson" {
		t.Errorf("Content-Type = %q, want application/x-ndjson", got)
	}
	if got := req.Headers.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
		t.Errorf("Authorization = %q, want Basic", got)
	}
	if got := req.Headers.Get("X-Wlog-Source"); got != "clickhouse" {
		t.Errorf("X-Wlog-Source = %q, want clickhouse", got)
	}

	lines := strings.Split(strings.TrimSpace(string(req.Body)), "\n")
	if len(lines) != 1 {
		t.Fatalf("body has %d lines, want 1: %s", len(lines), req.Body)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("row is not JSON: %v", err)
	}
	want := map[string]any{
		"timestamp": "2026-09-16T12:00:00Z", "level": "error", "operation": "payment.charge",
		"duration_ms": float64(42), "outcome": "error",
		"service_name": "orders", "service_version": "1.4.0", "service_env": "prod",
		"trace_id": "4bf9", "span_id": "00f0", "request_id": "req-1",
		"http_method": "POST", "http_route": "/pay/:id", "http_status": float64(500),
		"error_code": "PAYMENT_DECLINED", "error_message": "declined",
	}
	for key, value := range want {
		if row[key] != value {
			t.Errorf("row[%s] = %v, want %v", key, row[key], value)
		}
	}
	if row["event"] == nil {
		t.Error("row has no event column")
	}
}

// TestClickHouse_MissingValuesAreZero proves a minimal event still produces every
// column, with empty strings and zero numbers.
func TestClickHouse_MissingValuesAreZero(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	var row map[string]any
	if err := json.Unmarshal(srv.Last().Body, &row); err != nil {
		t.Fatalf("row is not JSON: %v", err)
	}
	for _, key := range []string{"operation", "service_name", "error_code", "http_route"} {
		if row[key] != "" {
			t.Errorf("row[%s] = %v, want an empty string", key, row[key])
		}
	}
	for _, key := range []string{"duration_ms", "http_status"} {
		if row[key] != float64(0) {
			t.Errorf("row[%s] = %v, want 0", key, row[key])
		}
	}
}

// TestClickHouse_EnvAlone proves the CLICKHOUSE_* env vars configure the drain.
func TestClickHouse_EnvAlone(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	t.Setenv("CLICKHOUSE_URL", srv.URL)
	t.Setenv("CLICKHOUSE_DATABASE", "env_db")
	t.Setenv("CLICKHOUSE_TABLE", "env_table")
	t.Setenv("CLICKHOUSE_USER", "u")
	t.Setenv("CLICKHOUSE_PASSWORD", "p")

	d, err := clickhouse.New()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"level": "info"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := srv.Last().Query.Get("query"); got != "INSERT INTO env_db.env_table FORMAT JSONEachRow" {
		t.Errorf("query = %q", got)
	}
}

// TestClickHouse_DDL proves the helper returns the recommended schema with every column.
func TestClickHouse_DDL(t *testing.T) {
	ddl := clickhouse.DDL("analytics.events")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS analytics.events",
		"timestamp DateTime64(9)",
		"level LowCardinality(String)",
		"http_status UInt16",
		"event JSON",
		"ENGINE = MergeTree",
		"ORDER BY timestamp",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("DDL is missing %q:\n%s", want, ddl)
		}
	}
	if again := clickhouse.DDL("analytics.events"); again != ddl {
		t.Error("DDL is not stable across calls")
	}
}

// TestClickHouse_NeverLeaksRedactedValue proves gate G1 end to end.
func TestClickHouse_NeverLeaksRedactedValue(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	log := wlog.New(wlog.WithDrains(pipeline.Wrap(d, pipeline.BatchSize(1))))
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "password", "hunter2")
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(srv.Requests()) == 0 {
		t.Fatal("no request reached the fake, so the leak check proved nothing")
	}
	for _, req := range srv.Requests() {
		if strings.Contains(string(req.Body), "hunter2") {
			t.Errorf("raw denied value reached the drain: %s", req.Body)
		}
	}
}
