// Package sqltest runs the shared database/sql behavior checks of wlogsql against one
// driver, so a fake driver and a real driver prove the same behavior.
//
// Read top to bottom: Factory opens one database and names the system every record must
// carry. Run checks the call record, the statement shape that hides a secret, the count
// of one row, and raw access to the driver connection.
//
// The checks stay inside the common SQL surface, so every driver of database/sql can run
// them. A driver that needs a server has no server on a development machine, and its
// factory skips the test with a message that names the environment variable.
package sqltest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/store/sqlshape"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// Factory opens one database over a driver that wlogsql wraps.
type Factory interface {
	// System returns the system every call record of this driver must carry.
	System() string
	// Open returns a database over the wrapped driver. It skips the test when the
	// driver needs a server that the machine does not have.
	Open(t *testing.T) *sql.DB
}

// Run runs every shared check against the factory.
func Run(t *testing.T, f Factory) {
	t.Helper()
	t.Run("CallRecord", func(t *testing.T) { testCallRecord(t, f) })
	t.Run("SecretShape", func(t *testing.T) { testSecretShape(t, f) })
	t.Run("TwoCalls", func(t *testing.T) { testTwoCalls(t, f) })
	t.Run("RawAccess", func(t *testing.T) { testRawAccess(t, f) })
}

// testCallRecord proves that one query gives one call record with the shape and the row
// count.
func testCallRecord(t *testing.T, f Factory) {
	log, rec := wlogtest.New(t)
	db := f.Open(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	var n int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	end()
	if n != 1 {
		t.Errorf("SELECT 1 = %d, want 1", n)
	}

	record := onlyCall(t, rec)
	want := sqlshape.Shape("SELECT 1", sqlshape.Unknown, 0)
	for key, value := range map[string]any{
		"kind": "db", "system": f.System(), "operation": "SELECT", "status": "ok",
		"target": want,
	} {
		if record[key] != value {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], value)
		}
	}
	if !conformance.Equal(record["rows"], int64(1)) {
		t.Errorf("calls[0].rows = %v, want 1", record["rows"])
	}
}

// testSecretShape proves that a literal never reaches the event.
func testSecretShape(t *testing.T, f Factory) {
	log, rec := wlogtest.New(t)
	db := f.Open(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	var secret string
	if err := db.QueryRowContext(ctx, "SELECT 'hunter2'").Scan(&secret); err != nil {
		t.Fatalf("query: %v", err)
	}
	end()

	record := onlyCall(t, rec)
	if record["target"] != sqlshape.Shape("SELECT 'hunter2'", sqlshape.Unknown, 0) {
		t.Errorf("calls[0].target = %v, want the shape", record["target"])
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "hunter2") {
		t.Errorf("the secret reached the event: %s", body)
	}
}

// testTwoCalls proves that every query gives its own record.
func testTwoCalls(t *testing.T, f Factory) {
	log, rec := wlogtest.New(t)
	db := f.Open(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	for i := 0; i < 2; i++ {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}
	end()

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
}

// testRawAccess proves that raw access reaches a connection of the driver.
func testRawAccess(t *testing.T, f Factory) {
	db := f.Open(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var inner driver.Conn
	err = conn.Raw(func(dc any) error {
		raw, ok := dc.(interface{ Raw() driver.Conn })
		if !ok {
			return fmt.Errorf("the wrapper connection has no Raw: %T", dc)
		}
		inner = raw.Raw()
		return nil
	})
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if inner == nil {
		t.Error("Raw returned no driver connection")
	}
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
