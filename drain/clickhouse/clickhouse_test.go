package clickhouse_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/clickhouse"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// newTestDrain points a Drain at a fake ClickHouse HTTP endpoint.
func newTestDrain(t *testing.T, srv *httpfake.Server, opts ...clickhouse.Option) *clickhouse.Sender {
	t.Helper()
	all := append([]clickhouse.Option{
		clickhouse.WithURL(srv.URL),
		clickhouse.WithBasicAuth("default", "pass"),
		clickhouse.WithDatabase("analytics"),
		clickhouse.WithTable("events"),
	}, opts...)
	d, err := clickhouse.NewSender(all...)
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
	if req.Method != http.MethodPost {
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
		"timestamp": "2026-09-16 12:00:00.000000000", "level": "error", "operation": "payment.charge",
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

	d, err := clickhouse.NewSender()
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
	ddl := clickhouse.DDL("analytics", "events")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS analytics.events",
		"timestamp DateTime64(9)",
		"duration_ms Float64",
		"level LowCardinality(String)",
		"http_status UInt16",
		"event String",
		"ENGINE = MergeTree",
		"ORDER BY timestamp",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("DDL is missing %q:\n%s", want, ddl)
		}
	}
	if again := clickhouse.DDL("analytics", "events"); again != ddl {
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

// TestClickHouse_PIPE9_TimestampFormat proves the row carries the DateTime64(9) text form
// in UTC, which is what ClickHouse accepts under its default input format.
func TestClickHouse_PIPE9_TimestampFormat(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv)

	event := fullEvent()
	event["timestamp"] = "2026-09-16T14:30:00.123456789+02:00"
	if err := d.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(srv.Last().Body))), &row); err != nil {
		t.Fatalf("row is not JSON: %v", err)
	}
	if got, want := row["timestamp"], "2026-09-16 12:30:00.123456789"; got != want {
		t.Errorf("timestamp = %v, want %q", got, want)
	}
}

// TestClickHouse_PIPE20_IdentifierRule proves a database or table name that is not a plain
// identifier is refused, so a config value can never inject SQL, and that DDL names both.
func TestClickHouse_PIPE20_IdentifierRule(t *testing.T) {
	bad := []struct {
		name     string
		database string
		table    string
	}{
		{"table with a statement", "analytics", "events; DROP TABLE users"},
		{"table with a dash", "analytics", "my-events"},
		{"database with a quote", "ana'lytics", "events"},
		{"empty-looking database", " ", "events"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := clickhouse.NewSender(
				clickhouse.WithURL("http://localhost:8123"),
				clickhouse.WithDatabase(tc.database),
				clickhouse.WithTable(tc.table),
			); err == nil {
				t.Fatalf("NewSender accepted database %q table %q", tc.database, tc.table)
			}
		})
	}

	if _, err := clickhouse.NewSender(
		clickhouse.WithURL("http://localhost:8123"),
		clickhouse.WithDatabase("analytics_2"),
		clickhouse.WithTable("events_v2"),
	); err != nil {
		t.Fatalf("NewSender refused valid identifiers: %v", err)
	}

	ddl := clickhouse.DDL("analytics", "events")
	if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS analytics.events") {
		t.Errorf("DDL does not name the database and the table:\n%s", ddl)
	}
	jsonDDL := clickhouse.DDLJSON("analytics", "events")
	if !strings.Contains(jsonDDL, "event JSON") {
		t.Errorf("DDLJSON has no JSON column:\n%s", jsonDDL)
	}
	if strings.Contains(ddl, "event JSON") {
		t.Errorf("DDL must stay on the String column:\n%s", ddl)
	}
}

// TestClickHouse_PIPE19_URLWithQuery proves a CLICKHOUSE_URL that already carries a query
// keeps it, instead of building a broken endpoint.
func TestClickHouse_PIPE19_URLWithQuery(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	d := newTestDrain(t, srv, clickhouse.WithURL(srv.URL+"?secure=true"))

	if err := d.SendBatch(context.Background(), []map[string]any{fullEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	if req == nil {
		t.Fatal("no request reached the fake")
	}
	if got := req.Query.Get("secure"); got != "true" {
		t.Errorf("the URL's own setting was lost: secure = %q", got)
	}
	if got := req.Query.Get("query"); got != "INSERT INTO analytics.events FORMAT JSONEachRow" {
		t.Errorf("query = %q", got)
	}
	if req.Path != "/" {
		t.Errorf("path = %q, want /", req.Path)
	}
}

// TestClickHouse_StatusTable proves every status class maps to the right outcome.
func TestClickHouse_StatusTable(t *testing.T) {
	cases := []struct {
		status    int
		wantError bool
		retryable bool
	}{
		{http.StatusOK, false, false},
		{http.StatusBadRequest, true, false},
		{http.StatusUnauthorized, true, false},
		{http.StatusForbidden, true, false},
		{http.StatusRequestEntityTooLarge, true, false},
		{http.StatusTooManyRequests, true, true},
		{http.StatusInternalServerError, true, true},
	}
	for _, tc := range cases {
		srv := httpfake.New()
		srv.SetStatus(tc.status)
		d := newTestDrain(t, srv)
		err := d.SendBatch(context.Background(), []map[string]any{fullEvent()})
		srv.Close()

		if !tc.wantError {
			if err != nil {
				t.Errorf("status %d: err = %v, want nil", tc.status, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("status %d: err = nil, want an error", tc.status)
			continue
		}
		var statusErr *httpdrain.StatusError
		if !errors.As(err, &statusErr) {
			t.Errorf("status %d: err = %v, want a *StatusError", tc.status, err)
			continue
		}
		if statusErr.Retryable() != tc.retryable {
			t.Errorf("status %d: Retryable() = %v, want %v", tc.status, statusErr.Retryable(), tc.retryable)
		}
	}
}
