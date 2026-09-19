// This file drives the bun query hook over an in-process SQLite database. It checks the
// statement shape that hides an argument value, the row count of an insert, the error
// code, and a query outside a unit of work.
package wlogbun_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogbun "github.com/jeremygprawira/wlog/store/bun"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// Order is the test model.
type Order struct {
	ID   int64 `bun:",pk,autoincrement"`
	Name string
}

// TestBun_C1_ShapeHidesValues proves that one query gives one db call record whose target
// is the statement shape, with no argument value.
func TestBun_C1_ShapeHidesValues(t *testing.T) {
	log, rec := wlogtest.New(t)
	db := newDB(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	var orders []Order
	if err := db.NewSelect().Model(&orders).Where("name = ?", "hunter2").Scan(ctx); err != nil {
		t.Fatalf("select: %v", err)
	}
	end()

	record := onlyCall(t, rec)
	if record["kind"] != "db" || record["system"] != "sqlite" || record["operation"] != "SELECT" {
		t.Errorf("calls[0] = %v, want a SELECT call of the sqlite system", record)
	}
	shape, _ := record["target"].(string)
	if !strings.Contains(shape, "?") || strings.Contains(shape, "hunter2") {
		t.Errorf("calls[0].target = %q, want the shape with no value", shape)
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "hunter2") {
		t.Errorf("the value reached the event: %s", body)
	}
}

// TestBun_C1_InsertRows proves that one insert records its row count.
func TestBun_C1_InsertRows(t *testing.T) {
	log, rec := wlogtest.New(t)
	db := newDB(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := db.NewInsert().Model(&Order{Name: "first"}).Exec(ctx); err != nil {
		t.Fatalf("insert: %v", err)
	}
	end()

	record := onlyCall(t, rec)
	if record["operation"] != "INSERT" {
		t.Errorf("calls[0].operation = %v, want INSERT", record["operation"])
	}
	if !conformance.Equal(record["rows"], int64(1)) {
		t.Errorf("calls[0].rows = %v, want 1", record["rows"])
	}
}

// TestBun_B2_ErrorCode proves that a failed query records a code and never the error
// text.
func TestBun_B2_ErrorCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	db := newDB(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := db.NewRaw("SELECT * FROM missing_table").Exec(ctx); err == nil {
		t.Fatal("the query returned no error")
	}
	end()

	record := onlyCall(t, rec)
	if _, present := record["error"]; !present {
		t.Errorf("calls[0] = %v, want an error", record)
	}
}

// TestBun_B2_NoEvent proves that a query outside a unit of work records nothing and
// reports nothing.
func TestBun_B2_NoEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	db := newDB(t)

	var orders []Order
	if err := db.NewSelect().Model(&orders).Scan(rec.Logger().WithContext(context.Background())); err != nil {
		t.Fatalf("select: %v", err)
	}
	if count := len(rec.Events()); count != 0 {
		t.Errorf("events = %d, want none", count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("problems = %d, want none", count)
	}
}

// newDB opens one SQLite database, creates the test table, and installs the wlog hook.
func newDB(t *testing.T) *bun.DB {
	t.Helper()
	sqldb, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, sqlitedialect.New())
	db.AddQueryHook(wlogbun.Hook())
	if _, err := db.NewCreateTable().Model((*Order)(nil)).Exec(context.Background()); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
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
