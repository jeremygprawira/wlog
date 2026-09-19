// This file drives the gorm plugin over a fake dialector and a fake driver that wlogsql
// wraps, so the tests need no server. The driver wrapper sits under the plugin, which
// proves that the pair records each query once.
package wloggorm_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wloggorm "github.com/jeremygprawira/wlog/store/gorm"
	wlogsql "github.com/jeremygprawira/wlog/store/sql"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// names numbers the registered fake drivers, because sql.Register refuses a name twice.
var names atomic.Int64

// TestGorm_C3_OneRecordPerQuery proves that gorm over Wrapped SQL records one call for
// one Create, and not one per layer.
func TestGorm_C3_OneRecordPerQuery(t *testing.T) {
	db, log, rec := openGorm(t, &fakeConn{})

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	tx := db.WithContext(ctx).Create(&Order{Name: "first"})
	if tx.Error != nil {
		t.Fatalf("create: %v", tx.Error)
	}
	end()

	record := onlyCall(t, rec)
	for key, want := range map[string]any{
		"kind": "db", "system": "fakesql", "operation": "INSERT", "target": "orders", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	attrs, _ := record["attrs"].(map[string]any)
	shape, _ := attrs["shape"].(string)
	if !strings.HasPrefix(shape, "INSERT INTO orders") {
		t.Errorf("calls[0].attrs.shape = %v, want the insert shape", shape)
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "first") {
		t.Errorf("the value reached the event: %s", body)
	}
}

// TestGorm_C1_Query proves that one Find records one SELECT call with its rows.
func TestGorm_C1_Query(t *testing.T) {
	db, log, rec := openGorm(t, &fakeConn{})

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	var orders []Order
	if tx := db.WithContext(ctx).Find(&orders); tx.Error != nil {
		t.Fatalf("find: %v", tx.Error)
	}
	end()

	record := onlyCall(t, rec)
	if record["operation"] != "SELECT" || record["target"] != "orders" {
		t.Errorf("calls[0] = %v, want a SELECT call on orders", record)
	}
	if !conformance.Equal(record["rows"], int64(1)) {
		t.Errorf("calls[0].rows = %v, want 1", record["rows"])
	}
}

// TestGorm_C3_RecordNotFound proves that a record gorm marks not found stays a success.
func TestGorm_C3_RecordNotFound(t *testing.T) {
	db, log, rec := openGorm(t, &emptyConn{})

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	var order Order
	tx := db.WithContext(ctx).First(&order)
	end()
	if !errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		t.Fatalf("First error = %v, want gorm.ErrRecordNotFound", tx.Error)
	}

	record := onlyCall(t, rec)
	if record["status"] != "ok" {
		t.Errorf("calls[0].status = %v, want ok", record["status"])
	}
	if _, present := record["error"]; present {
		t.Errorf("calls[0].error = %v, want no error", record["error"])
	}
}

// TestGorm_B2_FailedCall proves that a failed call records a code and never the error
// text.
func TestGorm_B2_FailedCall(t *testing.T) {
	db, log, rec := openGorm(t, &failingConn{err: stateError{code: "23505"}})

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	tx := db.WithContext(ctx).Create(&Order{Name: "first"})
	end()
	if tx.Error == nil {
		t.Fatal("create returned no error")
	}

	record := onlyCall(t, rec)
	callError, _ := record["error"].(map[string]any)
	if callError["code"] != "23505" {
		t.Errorf("calls[0].error.code = %v, want 23505", callError["code"])
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "duplicate key") {
		t.Errorf("the error text reached the event: %s", body)
	}
}

// openGorm opens one gorm database over a fake driver wrapped by wlogsql, with the wlog
// plugin installed.
func openGorm(t *testing.T, conn driver.Conn) (*gorm.DB, *wlog.Logger, *wlogtest.Recorder) {
	t.Helper()
	log, rec := wlogtest.New(t)
	name := fmt.Sprintf("wloggorm-fake-%d", names.Add(1))
	sql.Register(name, wlogsql.WrapDriver(fakeDriver{conn: conn}, wlogsql.WithSystem("fakesql")))
	pool, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	db, err := gorm.Open(fakeDialector{pool: pool}, &gorm.Config{
		Logger:               logger.Default.LogMode(logger.Silent),
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	if err := db.Use(wloggorm.Plugin()); err != nil {
		t.Fatalf("use the plugin: %v", err)
	}
	return db, log, rec
}

// onlyCall returns the only call record of the last event.
func onlyCall(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	return record
}

// Order is the test model.
type Order struct {
	ID   uint
	Name string
}

// TableName names the test table.
func (Order) TableName() string { return "orders" }

// fakeDialector is a gorm dialector over a prepared pool.
type fakeDialector struct {
	pool *sql.DB
}

// Name names the dialect.
func (fakeDialector) Name() string { return "fakesql" }

// Initialize registers the standard callbacks, as every real dialector does, and gives
// gorm the prepared pool.
func (d fakeDialector) Initialize(db *gorm.DB) error {
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
	db.ConnPool = d.pool
	return nil
}

// Migrator returns no migrator.
func (fakeDialector) Migrator(*gorm.DB) gorm.Migrator { return nil }

// DataTypeOf returns a text type for every field.
func (fakeDialector) DataTypeOf(*schema.Field) string { return "TEXT" }

// DefaultValueOf returns no default value.
func (fakeDialector) DefaultValueOf(*schema.Field) clause.Expression { return clause.Expr{SQL: "NULL"} }

// BindVarTo writes one placeholder.
func (fakeDialector) BindVarTo(writer clause.Writer, _ *gorm.Statement, _ interface{}) {
	_ = writer.WriteByte('?')
}

// QuoteTo writes one identifier with no quote.
func (fakeDialector) QuoteTo(writer clause.Writer, str string) { _, _ = writer.WriteString(str) }

// Explain returns the SQL unchanged.
func (fakeDialector) Explain(sql string, _ ...interface{}) string { return sql }

// fakeDriver opens the fake connection.
type fakeDriver struct {
	conn driver.Conn
}

// Open returns the fake connection.
func (d fakeDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

// fakeConn answers every statement with one affected row.
type fakeConn struct{}

// Prepare returns a statement that answers nothing.
func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return &fakeStmt{}, nil }

// Close closes the connection.
func (c *fakeConn) Close() error { return nil }

// Begin starts one transaction.
func (c *fakeConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }

// ExecContext reports one affected row.
func (c *fakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return fakeResult{rows: 1}, nil
}

// QueryContext returns one row with the columns of the test model.
func (c *fakeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{
		cols:   []string{"id", "name"},
		values: [][]driver.Value{{int64(1), "first"}},
	}, nil
}

// emptyConn answers every query with no row.
type emptyConn struct{ fakeConn }

// QueryContext returns no row.
func (c *emptyConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{cols: []string{"id", "name"}}, nil
}

// failingConn fails every statement.
type failingConn struct {
	fakeConn
	err error
}

// ExecContext returns the test error.
func (c *failingConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, c.err
}

// fakeStmt is the prepared statement of the fake connection.
type fakeStmt struct{}

// Close closes the statement.
func (s *fakeStmt) Close() error { return nil }

// NumInput reports an unknown count.
func (s *fakeStmt) NumInput() int { return -1 }

// Exec reports one affected row.
func (s *fakeStmt) Exec([]driver.Value) (driver.Result, error) { return fakeResult{rows: 1}, nil }

// Query returns no row.
func (s *fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	return &fakeRows{cols: []string{"id", "name"}}, nil
}

// fakeResult is the result of one fake statement.
type fakeResult struct{ rows int64 }

// LastInsertId returns the fake insert id.
func (r fakeResult) LastInsertId() (int64, error) { return 1, nil }

// RowsAffected returns the fake row count.
func (r fakeResult) RowsAffected() (int64, error) { return r.rows, nil }

// fakeTx is one fake transaction.
type fakeTx struct{}

// Commit commits the transaction.
func (fakeTx) Commit() error { return nil }

// Rollback rolls the transaction back.
func (fakeTx) Rollback() error { return nil }

// fakeRows is one fake row set.
type fakeRows struct {
	cols   []string
	values [][]driver.Value
	index  int
}

// Columns returns the column names.
func (r *fakeRows) Columns() []string { return r.cols }

// Close closes the row set.
func (r *fakeRows) Close() error { return nil }

// Next copies the next row.
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

// stateError is a driver error that carries a SQLSTATE.
type stateError struct{ code string }

// Error returns the error text.
func (e stateError) Error() string { return "duplicate key value violates unique constraint" }

// SQLState returns the SQL state of the error.
func (e stateError) SQLState() string { return e.code }
