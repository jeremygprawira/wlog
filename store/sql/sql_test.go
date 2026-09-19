// This file checks the database/sql wrapper against a fake driver: the call record and
// its shape, the ErrSkip sentinel, the NamedValueChecker fallback, the optional interface
// selection, column types, Raw access, and the transaction rules.
//
// Every fake type carries one optional interface on purpose, because database/sql
// inspects the optional interfaces of a driver and changes behavior when one appears.
package wlogsql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogsql "github.com/jeremygprawira/wlog/store/sql"
	"github.com/jeremygprawira/wlog/store/sql/sqltest"
	"github.com/jeremygprawira/wlog/store/sqlshape"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// names numbers the registered fake drivers, because sql.Register refuses a name twice.
var names atomic.Int64

// TestSQL_C1_CallRecord proves that one query gives one call record with the statement
// shape, the operation, the row count, and no secret.
func TestSQL_C1_CallRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &queryConn{
		fakeConn: &fakeConn{},
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return &fakeRows{cols: []string{"id"}, values: [][]driver.Value{{int64(1)}, {int64(2)}}}, nil
		},
	}
	db := openDB(t, conn, wlogsql.WithSystem("postgresql"))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	const query = "SELECT id FROM orders WHERE token = 'hunter2' AND id = ?"
	rows, err := db.QueryContext(ctx, query, 42)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	end()

	record := onlyCall(t, rec)
	want := sqlshape.Shape(query, sqlshape.Unknown, 0)
	for key, want := range map[string]any{
		"kind": "db", "system": "postgresql", "operation": "SELECT", "status": "ok",
		"target": want,
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	if !conformance.Equal(record["rows"], int64(2)) {
		t.Errorf("calls[0].rows = %v, want 2", record["rows"])
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "hunter2") {
		t.Errorf("the secret reached the event: %s", body)
	}
}

// TestSQL_B2_ErrSkipSentinel proves that a skipped exec records nothing, that the
// prepared call records the work, and that the result reaches the caller unchanged.
func TestSQL_B2_ErrSkipSentinel(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &execConn{
		fakeConn: &fakeConn{prepare: func(query string) (driver.Stmt, error) {
			return &fakeStmt{query: query, exec: func([]driver.Value) (driver.Result, error) {
				return fakeResult{rows: 1}, nil
			}}, nil
		}},
		exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return nil, driver.ErrSkip
		},
	}
	db := openDB(t, conn, wlogsql.WithSystem("postgresql"))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	res, err := db.ExecContext(ctx, "UPDATE orders SET status = ? WHERE id = ?", "paid", 7)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if rows, err := res.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("RowsAffected = %d, %v, want 1", rows, err)
	}
	end()

	record := onlyCall(t, rec)
	if record["operation"] != "UPDATE" {
		t.Errorf("calls[0].operation = %v, want UPDATE", record["operation"])
	}
}

// TestSQL_B2_NamedValueCheckerFallback proves that the wrapper statement checker falls
// back to the checker of the connection, and that a driver with no checker still gets
// the database/sql conversion error.
func TestSQL_B2_NamedValueCheckerFallback(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &checkerConn{
		fakeConn: &fakeConn{prepare: func(query string) (driver.Stmt, error) {
			return &fakeStmt{query: query, exec: func(args []driver.Value) (driver.Result, error) {
				if len(args) != 1 || args[0] != int64(500) {
					return nil, fmt.Errorf("the driver saw %v, want the converted int64", args)
				}
				return fakeResult{rows: 1}, nil
			}}, nil
		}},
		check: func(nv *driver.NamedValue) error {
			if value, ok := nv.Value.(money); ok {
				nv.Value = value.cents
				return nil
			}
			return driver.ErrSkip
		},
	}
	db := openDB(t, conn, wlogsql.WithSystem("postgresql"))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := db.ExecContext(ctx, "INSERT INTO payments VALUES (?)", money{cents: 500}); err != nil {
		t.Fatalf("exec through the connection checker: %v", err)
	}
	end()
	onlyCall(t, rec)

	plain := openDB(t, &fakeConn{})
	if _, err := plain.ExecContext(context.Background(), "INSERT INTO payments VALUES (?)", money{cents: 500}); err == nil ||
		!strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("error = %v, want the database/sql unsupported type error", err)
	}
}

// TestSQL_B2_InterfaceSelection proves that the connection wrapper carries a session
// resetter and a validator exactly when the driver does, because database/sql reads those
// two interfaces to decide whether it keeps a connection after a rollback.
func TestSQL_B2_InterfaceSelection(t *testing.T) {
	cases := []struct {
		name  string
		conn  driver.Conn
		reset bool
		valid bool
	}{
		{"plain", &fakeConn{}, false, false},
		{"resetter", &resetterConn{fakeConn: &fakeConn{}}, true, false},
		{"validator", &validatorConn{fakeConn: &fakeConn{}}, false, true},
		{"both", &bothConn{fakeConn: &fakeConn{}}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := openDriver(t, tc.conn)
			if _, ok := conn.(driver.SessionResetter); ok != tc.reset {
				t.Errorf("SessionResetter = %v, want %v", ok, tc.reset)
			}
			if _, ok := conn.(driver.Validator); ok != tc.valid {
				t.Errorf("Validator = %v, want %v", ok, tc.valid)
			}
		})
	}
}

// TestSQL_B2_ColumnConverterConditional proves that the statement wrapper carries the
// column converter of the driver only when the driver has one.
func TestSQL_B2_ColumnConverterConditional(t *testing.T) {
	plain := openDriver(t, &fakeConn{})
	stmt, err := plain.Prepare("SELECT 1")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, ok := stmt.(driver.ColumnConverter); ok { //nolint:staticcheck // the test reads the deprecated interface on purpose
		t.Error("the wrapper added a column converter the driver does not have")
	}

	withConverter := openDriver(t, &fakeConn{prepare: func(query string) (driver.Stmt, error) {
		return &converterStmt{fakeStmt: &fakeStmt{query: query}}, nil
	}})
	stmt, err = withConverter.Prepare("SELECT 1")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, ok := stmt.(driver.ColumnConverter); !ok { //nolint:staticcheck // the test reads the deprecated interface on purpose
		t.Error("the column converter of the driver is missing")
	}
}

// TestSQL_B2_ColumnTypes proves that the column type methods of the driver reach
// database/sql through the wrapper, and that a driver without them reports nothing.
func TestSQL_B2_ColumnTypes(t *testing.T) {
	conn := &queryConn{
		fakeConn: &fakeConn{},
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return &typedRows{
				fakeRows: &fakeRows{cols: []string{"id"}},
				scan:     reflect.TypeOf(int64(0)),
				nullable: true, length: 12, dbType: "INT8", precision: 10, scale: 2,
			}, nil
		},
	}
	db := openDB(t, conn)
	rows, err := db.Query("SELECT id FROM orders")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatalf("column types: %v", err)
	}
	column := types[0]
	if column.ScanType() != reflect.TypeOf(int64(0)) {
		t.Errorf("ScanType = %v, want int64", column.ScanType())
	}
	if column.DatabaseTypeName() != "INT8" {
		t.Errorf("DatabaseTypeName = %v, want INT8", column.DatabaseTypeName())
	}
	if nullable, ok := column.Nullable(); !ok || !nullable {
		t.Errorf("Nullable = %v, %v, want true", nullable, ok)
	}
	if length, ok := column.Length(); !ok || length != 12 {
		t.Errorf("Length = %v, %v, want 12", length, ok)
	}
	if precision, scale, ok := column.DecimalSize(); !ok || precision != 10 || scale != 2 {
		t.Errorf("DecimalSize = %v, %v, %v, want 10, 2", precision, scale, ok)
	}

	plain := openDB(t, &queryConn{fakeConn: &fakeConn{}, query: func(string, []driver.NamedValue) (driver.Rows, error) {
		return &fakeRows{cols: []string{"id"}}, nil
	}})
	rows, err = plain.Query("SELECT id FROM orders")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	types, _ = rows.ColumnTypes()
	if types[0].ScanType() != nil || types[0].DatabaseTypeName() != "" {
		t.Errorf("a driver with no column types reported %v, %v", types[0].ScanType(), types[0].DatabaseTypeName())
	}
}

// TestSQL_B2_RawAccess proves that raw access reaches the connection of the driver.
func TestSQL_B2_RawAccess(t *testing.T) {
	inner := &fakeConn{}
	db := openDB(t, inner)

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var got driver.Conn
	err = conn.Raw(func(dc any) error {
		raw, ok := dc.(interface{ Raw() driver.Conn })
		if !ok {
			return errors.New("the wrapper connection has no Raw")
		}
		got = raw.Raw()
		return nil
	})
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if got != driver.Conn(inner) {
		t.Errorf("Raw returned %T, want the driver connection", got)
	}
}

// TestSQL_B2_BeginTxRules proves that a driver with no ConnBeginTx keeps the isolation
// and read-only errors of database/sql, and that a driver with one gets the options.
func TestSQL_B2_BeginTxRules(t *testing.T) {
	plain := openDB(t, &fakeConn{})
	if _, err := plain.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable}); err == nil ||
		!strings.Contains(err.Error(), "non-default isolation level") {
		t.Errorf("isolation error = %v, want the database/sql error", err)
	}
	if _, err := plain.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true}); err == nil ||
		!strings.Contains(err.Error(), "read-only") {
		t.Errorf("read-only error = %v, want the database/sql error", err)
	}

	var got driver.TxOptions
	conn := &beginTxConn{fakeConn: &fakeConn{}, beginTx: func(opts driver.TxOptions) (driver.Tx, error) {
		got = opts
		return fakeTx{}, nil
	}}
	db := openDB(t, conn)
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got.Isolation != driver.IsolationLevel(sql.LevelSerializable) || !got.ReadOnly {
		t.Errorf("the driver saw %+v, want the isolation level and read only", got)
	}
}

// TestSQL_B2_PrepareIsNotACall proves that prepare records nothing, and that the exec of
// the prepared statement records one call.
func TestSQL_B2_PrepareIsNotACall(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &fakeConn{prepare: func(query string) (driver.Stmt, error) {
		return &fakeStmt{query: query, exec: func([]driver.Value) (driver.Result, error) {
			return fakeResult{rows: 1}, nil
		}}, nil
	}}
	db := openDB(t, conn, wlogsql.WithSystem("postgresql"))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	stmt, err := db.PrepareContext(ctx, "UPDATE orders SET status = ? WHERE id = ?")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := stmt.ExecContext(ctx, "paid", 7); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if err := stmt.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	end()

	onlyCall(t, rec)
}

// TestSQL_B2_NoEvent proves that a call outside a unit of work records nothing and
// reports nothing.
func TestSQL_B2_NoEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	conn := &execConn{
		fakeConn: &fakeConn{},
		exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return fakeResult{rows: 1}, nil
		},
	}
	db := openDB(t, conn)

	ctx := rec.Logger().WithContext(context.Background())
	if _, err := db.ExecContext(ctx, "UPDATE orders SET status = ?", "paid"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if count := len(rec.Events()); count != 0 {
		t.Errorf("events = %d, want none", count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("problems = %d, want none", count)
	}
}

// TestSQL_B2_LateRowsRecordNothing proves that rows closed after the event emitted
// record nothing, and that the close does not panic.
func TestSQL_B2_LateRowsRecordNothing(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &queryConn{
		fakeConn: &fakeConn{},
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return &fakeRows{cols: []string{"id"}, values: [][]driver.Value{{int64(1)}}}, nil
		},
	}
	db := openDB(t, conn)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	rows, err := db.QueryContext(ctx, "SELECT id FROM orders")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	end()
	if err := rows.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := rec.Last()["calls"]; ok {
		t.Errorf("a record reached an event that already emitted: %v", rec.Last())
	}
}

// TestSQL_B2_WrapConnector proves that a connector and a driver context both reach the
// wrapper, and that the call record still appears.
func TestSQL_B2_WrapConnector(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &execConn{
		fakeConn: &fakeConn{},
		exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return fakeResult{rows: 1}, nil
		},
	}
	wrapped := wlogsql.Wrap(&fakeConnector{conn: conn}, wlogsql.WithSystem("mysql"))
	db := sql.OpenDB(wrapped)
	t.Cleanup(func() { _ = db.Close() })

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := db.ExecContext(ctx, "UPDATE orders SET status = ?", "paid"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	end()
	record := onlyCall(t, rec)
	if record["system"] != "mysql" {
		t.Errorf("calls[0].system = %v, want mysql", record["system"])
	}

	// A DriverContext driver also opens through its connector.
	driverContext := wlogsql.WrapDriver(&fakeContextDriver{conn: conn})
	if _, err := driverContext.Open("dsn"); err != nil {
		t.Fatalf("open through the driver context: %v", err)
	}
	if _, err := driverContext.(driver.DriverContext).OpenConnector("dsn"); err != nil {
		t.Fatalf("open connector: %v", err)
	}
}

// TestSQL_B2_Ping proves that a driver with a pinger reaches it, and that a driver
// without one reports success.
func TestSQL_B2_Ping(t *testing.T) {
	pinged := false
	conn := &pingerConn{fakeConn: &fakeConn{}, ping: func() error { pinged = true; return nil }}
	db := openDB(t, conn)
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if !pinged {
		t.Error("the pinger of the driver was not called")
	}

	plain := openDB(t, &fakeConn{})
	if err := plain.Ping(); err != nil {
		t.Errorf("ping with no pinger: %v", err)
	}
}

// TestSQL_B2_ErrorCode proves that a driver error with a SQLSTATE records the code and
// never the error text.
func TestSQL_B2_ErrorCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &execConn{
		fakeConn: &fakeConn{},
		exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return nil, stateError{code: "23505"}
		},
	}
	db := openDB(t, conn)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := db.ExecContext(ctx, "INSERT INTO orders VALUES (?)", 1); err == nil {
		t.Fatal("exec returned no error")
	}
	end()

	record := onlyCall(t, rec)
	callError, _ := record["error"].(map[string]any)
	if callError["code"] != "23505" {
		t.Errorf("calls[0].error.code = %v, want 23505", callError["code"])
	}
	if _, present := callError["message"]; present {
		t.Errorf("calls[0].error.message = %v, want no error text", callError["message"])
	}
}

// TestSQL_B2_BeginFallback proves that a plain Begin reaches the driver when the driver
// has no ConnBeginTx.
func TestSQL_B2_BeginFallback(t *testing.T) {
	began := false
	db := openDB(t, &fakeConn{begin: func() (driver.Tx, error) { began = true; return fakeTx{}, nil }})
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if !began {
		t.Error("the driver Begin was not called")
	}
}

// TestSQL_B2_RowsCloseError proves that a close error ends the call with a code.
func TestSQL_B2_RowsCloseError(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &queryConn{
		fakeConn: &fakeConn{},
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return &closeErrorRows{fakeRows: &fakeRows{cols: []string{"id"}}}, nil
		},
	}
	db := openDB(t, conn)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	rows, err := db.QueryContext(ctx, "SELECT id FROM orders")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if err := rows.Close(); err == nil {
		t.Fatal("close returned no error")
	}
	end()

	record := onlyCall(t, rec)
	callError, _ := record["error"].(map[string]any)
	if callError["code"] != "error" {
		t.Errorf("calls[0].error = %v, want a code", record["error"])
	}
}

// TestSQL_B2_DriverLevel proves the wrapper methods that database/sql reaches only for a
// driver with the matching optional interface, plus the methods the driver.Stmt interface
// requires but database/sql never calls.
func TestSQL_B2_DriverLevel(t *testing.T) {
	conn := openDriver(t, &bothConn{fakeConn: &fakeConn{prepare: func(query string) (driver.Stmt, error) {
		return &converterStmt{fakeStmt: &fakeStmt{query: query}}, nil
	}}})
	resetter, ok := conn.(driver.SessionResetter)
	if !ok {
		t.Fatal("the wrapper connection has no session reset")
	}
	if err := resetter.ResetSession(context.Background()); err != nil {
		t.Errorf("ResetSession: %v", err)
	}
	validator, ok := conn.(driver.Validator)
	if !ok {
		t.Fatal("the wrapper connection has no validator")
	}
	if !validator.IsValid() {
		t.Error("IsValid = false, want true")
	}
	tx, err := conn.Begin() //nolint:staticcheck // a driver without ConnBeginTx has only Begin
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	preparer, ok := conn.(driver.ConnPrepareContext)
	if !ok {
		t.Fatal("the wrapper connection has no context prepare")
	}
	stmt, err := preparer.PrepareContext(context.Background(), "UPDATE orders SET status = ?")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := stmt.Exec([]driver.Value{"paid"}); err != nil { //nolint:staticcheck // a driver without StmtExecContext has only Exec
		t.Errorf("exec: %v", err)
	}
	rows, err := stmt.Query([]driver.Value{"paid"}) //nolint:staticcheck // a driver without StmtQueryContext has only Query
	if err != nil {
		t.Errorf("query: %v", err)
	} else if err := rows.Close(); err != nil {
		t.Errorf("close rows: %v", err)
	}
	if converter, ok := stmt.(driver.ColumnConverter); !ok { //nolint:staticcheck // the test reads the deprecated interface on purpose
		t.Error("the column converter of the driver is missing")
	} else if converter.ColumnConverter(0) == nil {
		t.Error("ColumnConverter returned nil")
	}
}

// TestSQL_B2_QueryError proves that a failed query ends the call with a code and returns
// the error of the driver unchanged.
func TestSQL_B2_QueryError(t *testing.T) {
	log, rec := wlogtest.New(t)
	failure := stateError{code: "42P01"}
	conn := &queryConn{
		fakeConn: &fakeConn{},
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return nil, failure
		},
	}
	db := openDB(t, conn)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := db.QueryContext(ctx, "SELECT id FROM missing"); !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the driver error", err)
	}
	end()

	record := onlyCall(t, rec)
	callError, _ := record["error"].(map[string]any)
	if callError["code"] != "42P01" {
		t.Errorf("calls[0].error = %v, want the SQLSTATE", record["error"])
	}
}

// TestSQL_B2_StmtQueryFallback proves that a prepared query through a statement with no
// StmtQueryContext records the call.
func TestSQL_B2_StmtQueryFallback(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &fakeConn{prepare: func(query string) (driver.Stmt, error) {
		return &fakeStmt{query: query}, nil
	}}
	db := openDB(t, conn)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	rows, err := db.QueryContext(ctx, "SELECT id FROM orders")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	end()

	record := onlyCall(t, rec)
	if record["operation"] != "SELECT" {
		t.Errorf("calls[0].operation = %v, want SELECT", record["operation"])
	}
}

// TestSQL_B2_DefaultSystem proves that the system of a record is the driver type name
// when no option names it.
func TestSQL_B2_DefaultSystem(t *testing.T) {
	log, rec := wlogtest.New(t)
	conn := &execConn{
		fakeConn: &fakeConn{},
		exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return fakeResult{rows: 1}, nil
		},
	}
	db := openDB(t, conn)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := db.ExecContext(ctx, "UPDATE orders SET status = ?", "paid"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	end()

	record := onlyCall(t, rec)
	if record["system"] != "wlogsql_test.fakeDriver" {
		t.Errorf("calls[0].system = %v, want the driver type name", record["system"])
	}
}

// TestSQL_B2_ErrorPaths proves the connector errors, the prepare and begin fallbacks,
// and the argument conversion errors of the wrapper.
func TestSQL_B2_ErrorPaths(t *testing.T) {
	// A single optional interface still selects its wrapper type.
	resetter := openDriver(t, &resetterConn{fakeConn: &fakeConn{}})
	if err := resetter.(driver.SessionResetter).ResetSession(context.Background()); err != nil {
		t.Errorf("ResetSession: %v", err)
	}
	validator := openDriver(t, &validatorConn{fakeConn: &fakeConn{}})
	if !validator.(driver.Validator).IsValid() {
		t.Error("IsValid = false, want true")
	}

	// The connector returns the errors of its driver.
	failure := errors.New("the database is down")
	failing := &failingConnector{err: failure}
	wrapped := wlogsql.Wrap(failing)
	if _, err := wrapped.Connect(context.Background()); !errors.Is(err, failure) {
		t.Errorf("Connect = %v, want the connector error", err)
	}
	if wrapped.Driver() == nil {
		t.Error("Driver returned nil")
	}

	// A prepare error and a canceled context reach the caller.
	prepareFailure := errors.New("the statement is bad")
	conn := openDriver(t, &fakeConn{prepare: func(string) (driver.Stmt, error) { return nil, prepareFailure }})
	if _, err := conn.Prepare("SELECT 1"); !errors.Is(err, prepareFailure) {
		t.Errorf("Prepare = %v, want the driver error", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	preparer := conn.(driver.ConnPrepareContext)
	if _, err := preparer.PrepareContext(canceled, "SELECT 1"); !errors.Is(err, prepareFailure) {
		t.Errorf("PrepareContext = %v, want the driver error", err)
	}
	ok := openDriver(t, &fakeConn{})
	preparer = ok.(driver.ConnPrepareContext)
	if _, err := preparer.PrepareContext(canceled, "SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Errorf("PrepareContext after cancel = %v, want context.Canceled", err)
	}

	// Begin reports a canceled context after the driver began.
	began := false
	beginConn := openDriver(t, &fakeConn{begin: func() (driver.Tx, error) { began = true; return fakeTx{}, nil }})
	tx, err := beginConn.(driver.ConnBeginTx).BeginTx(canceled, driver.TxOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("BeginTx after cancel = %v, want context.Canceled", err)
	}
	if tx != nil {
		t.Errorf("BeginTx returned %v, want no transaction", tx)
	}
	if !began {
		t.Error("the driver Begin was not called")
	}
}

// TestSQL_B2_ArgumentErrors proves that a named argument a driver cannot take reports the
// database/sql error, and that the statement checker returns what the driver checker
// returned.
func TestSQL_B2_ArgumentErrors(t *testing.T) {
	checker := &checkerConn{
		fakeConn: &fakeConn{prepare: func(query string) (driver.Stmt, error) {
			return &stmtChecker{fakeStmt: &fakeStmt{query: query}}, nil
		}},
		check: func(*driver.NamedValue) error { return nil },
	}
	db := openDB(t, checker)
	_, err := db.ExecContext(context.Background(), "INSERT INTO orders VALUES (?)", sql.Named("id", 1))
	if err == nil || !strings.Contains(err.Error(), "Named Parameters") {
		t.Errorf("exec = %v, want the named parameter error", err)
	}
	if _, err := db.QueryContext(context.Background(), "SELECT id FROM orders WHERE id = ?", sql.Named("id", 1)); err == nil ||
		!strings.Contains(err.Error(), "Named Parameters") {
		t.Errorf("query = %v, want the named parameter error", err)
	}

	// The statement checker of the driver answers first.
	conn := &fakeConn{prepare: func(query string) (driver.Stmt, error) {
		return &stmtChecker{fakeStmt: &fakeStmt{query: query}}, nil
	}}
	stmt, err := openDriver(t, conn).Prepare("INSERT INTO orders VALUES (?)")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	nvc, ok := stmt.(driver.NamedValueChecker)
	if !ok {
		t.Fatal("the wrapper statement has no checker")
	}
	if err := nvc.CheckNamedValue(&driver.NamedValue{Ordinal: 1, Name: "id", Value: int64(1)}); err != nil {
		t.Errorf("CheckNamedValue = %v, want the stmt checker result", err)
	}
}

// TestSQL_B2_Suite runs the shared driver checks against the fake driver, so the suite is
// proven before a real driver runs it.
func TestSQL_B2_Suite(t *testing.T) {
	sqltest.Run(t, fakeFactory{})
}

// fakeFactory opens the fake driver for the shared suite.
type fakeFactory struct{}

// System returns the system of the fake records.
func (fakeFactory) System() string { return "fake" }

// Open returns a database over the fake driver.
func (fakeFactory) Open(t *testing.T) *sql.DB {
	t.Helper()
	conn := &queryConn{
		fakeConn: &fakeConn{},
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return &fakeRows{cols: []string{"n"}, values: [][]driver.Value{{int64(1)}}}, nil
		},
	}
	return openDB(t, conn, wlogsql.WithSystem("fake"))
}

// openDB registers one fake driver under a fresh name and opens it, wrapped in wlogsql.
func openDB(t *testing.T, conn driver.Conn, opts ...wlogsql.Option) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("wlogsql-fake-%d", names.Add(1))
	sql.Register(name, wlogsql.WrapDriver(fakeDriver{conn: conn}, opts...))
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// openDriver returns the wrapped connection of one fake driver, with no database/sql
// above it.
func openDriver(t *testing.T, conn driver.Conn) driver.Conn {
	t.Helper()
	wrapped := wlogsql.WrapDriver(fakeDriver{conn: conn})
	driverConn, err := wrapped.Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return driverConn
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

// fakeDriver opens one connection the test built.
type fakeDriver struct {
	conn driver.Conn
}

// Open returns the test connection.
func (d fakeDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

// fakeConn is a driver connection with no optional interface.
type fakeConn struct {
	prepare func(query string) (driver.Stmt, error)
	begin   func() (driver.Tx, error)
}

// Prepare returns the statement the test built.
func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	if c.prepare != nil {
		return c.prepare(query)
	}
	return &fakeStmt{query: query}, nil
}

// Close closes the connection.
func (c *fakeConn) Close() error { return nil }

// Begin starts one transaction.
func (c *fakeConn) Begin() (driver.Tx, error) {
	if c.begin != nil {
		return c.begin()
	}
	return fakeTx{}, nil
}

// fakeStmt is a driver statement with no optional interface.
type fakeStmt struct {
	query     string
	exec      func(args []driver.Value) (driver.Result, error)
	queryRows func(args []driver.Value) (driver.Rows, error)
}

// Close closes the statement.
func (s *fakeStmt) Close() error { return nil }

// NumInput reports an unknown count.
func (s *fakeStmt) NumInput() int { return -1 }

// Exec runs one statement.
func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	if s.exec != nil {
		return s.exec(args)
	}
	return fakeResult{rows: 1}, nil
}

// Query runs one query.
func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	if s.queryRows != nil {
		return s.queryRows(args)
	}
	return &fakeRows{cols: []string{"id"}}, nil
}

// fakeResult is the result of one fake statement.
type fakeResult struct {
	rows int64
	id   int64
}

// LastInsertId returns the fake insert id.
func (r fakeResult) LastInsertId() (int64, error) { return r.id, nil }

// RowsAffected returns the fake row count.
func (r fakeResult) RowsAffected() (int64, error) { return r.rows, nil }

// fakeTx is one fake transaction.
type fakeTx struct{}

// Commit commits the transaction.
func (fakeTx) Commit() error { return nil }

// Rollback rolls the transaction back.
func (fakeTx) Rollback() error { return nil }

// fakeRows is one fake row set with no column type information.
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

// typedRows adds the column type methods of a driver.
type typedRows struct {
	*fakeRows
	scan      reflect.Type
	nullable  bool
	length    int64
	dbType    string
	precision int64
	scale     int64
}

// ColumnTypeScanType returns the scan type of one column.
func (r *typedRows) ColumnTypeScanType(int) reflect.Type { return r.scan }

// ColumnTypeNullable reports whether one column can be null.
func (r *typedRows) ColumnTypeNullable(int) (bool, bool) { return r.nullable, true }

// ColumnTypeLength returns the length of one column.
func (r *typedRows) ColumnTypeLength(int) (int64, bool) { return r.length, true }

// ColumnTypeDatabaseTypeName returns the database type name of one column.
func (r *typedRows) ColumnTypeDatabaseTypeName(int) string { return r.dbType }

// ColumnTypePrecisionScale returns the precision and scale of one column.
func (r *typedRows) ColumnTypePrecisionScale(int) (int64, int64, bool) {
	return r.precision, r.scale, true
}

// money is a value type that only a NamedValueChecker knows how to convert.
type money struct {
	cents int64
}

// execConn adds ExecerContext of a driver.
type execConn struct {
	*fakeConn
	exec func(query string, args []driver.NamedValue) (driver.Result, error)
}

// ExecContext runs one statement.
func (c *execConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.exec(query, args)
}

// queryConn adds QueryerContext of a driver.
type queryConn struct {
	*fakeConn
	query func(query string, args []driver.NamedValue) (driver.Rows, error)
}

// QueryContext runs one query.
func (c *queryConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.query(query, args)
}

// checkerConn adds NamedValueChecker of a driver.
type checkerConn struct {
	*fakeConn
	check func(*driver.NamedValue) error
}

// CheckNamedValue converts one argument.
func (c *checkerConn) CheckNamedValue(nv *driver.NamedValue) error { return c.check(nv) }

// resetterConn adds SessionResetter of a driver.
type resetterConn struct{ *fakeConn }

// ResetSession resets the session.
func (c *resetterConn) ResetSession(context.Context) error { return nil }

// validatorConn adds Validator of a driver.
type validatorConn struct{ *fakeConn }

// IsValid reports that the connection is usable.
func (c *validatorConn) IsValid() bool { return true }

// bothConn adds SessionResetter and Validator of a driver.
type bothConn struct{ *fakeConn }

// ResetSession resets the session.
func (c *bothConn) ResetSession(context.Context) error { return nil }

// IsValid reports that the connection is usable.
func (c *bothConn) IsValid() bool { return true }

// beginTxConn adds ConnBeginTx of a driver.
type beginTxConn struct {
	*fakeConn
	beginTx func(opts driver.TxOptions) (driver.Tx, error)
}

// BeginTx starts one transaction with the options.
func (c *beginTxConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.beginTx(opts)
}

// converterStmt adds ColumnConverter of a driver.
type converterStmt struct{ *fakeStmt }

// ColumnConverter returns the converter of one column.
func (s *converterStmt) ColumnConverter(int) driver.ValueConverter {
	return driver.DefaultParameterConverter
}

// fakeConnector is a driver connector over one fake connection.
type fakeConnector struct {
	conn driver.Conn
}

// Connect returns the test connection.
func (c *fakeConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }

// Driver returns the driver of the connector.
func (c *fakeConnector) Driver() driver.Driver { return fakeDriver{conn: c.conn} }

// fakeContextDriver is a driver with DriverContext and no plain Open.
type fakeContextDriver struct {
	conn driver.Conn
}

// OpenConnector returns a connector over the test connection.
func (d *fakeContextDriver) OpenConnector(string) (driver.Connector, error) {
	return &fakeConnector{conn: d.conn}, nil
}

// Open returns the test connection.
func (d *fakeContextDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

// pingerConn adds Pinger of a driver.
type pingerConn struct {
	*fakeConn
	ping func() error
}

// Ping pings the connection.
func (c *pingerConn) Ping(context.Context) error { return c.ping() }

// stateError is a driver error that carries a SQLSTATE.
type stateError struct{ code string }

// Error returns the error text.
func (e stateError) Error() string { return "duplicate key value violates unique constraint" }

// SQLState returns the SQL state of the error.
func (e stateError) SQLState() string { return e.code }

// closeErrorRows is a row set whose close fails.
type closeErrorRows struct{ *fakeRows }

// Close returns an error instead of closing.
func (r *closeErrorRows) Close() error { return errors.New("the connection is gone") }

// failingConnector returns an error from Connect.
type failingConnector struct{ err error }

// Connect returns the error of the test.
func (c *failingConnector) Connect(context.Context) (driver.Conn, error) { return nil, c.err }

// Driver returns a driver over the same connection.
func (c *failingConnector) Driver() driver.Driver { return fakeDriver{} }

// stmtChecker adds NamedValueChecker of a driver statement.
type stmtChecker struct{ *fakeStmt }

// CheckNamedValue accepts every argument.
func (s *stmtChecker) CheckNamedValue(*driver.NamedValue) error { return nil }
