// This file drives the pgx tracer without a server: every trace method takes plain data,
// so a test calls it the way pgx would. It checks the call record of a query, a batch,
// and a copy, the code of a failed query, the forwarding to the tracer of the app, and
// the prepare that records nothing.
package wlogpgx_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogpgx "github.com/jeremygprawira/wlog/store/pgx"
	"github.com/jeremygprawira/wlog/store/sqlshape"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestPgx_C1_QueryCall proves that one query gives one db call record with the shape and
// the row count, and no argument.
func TestPgx_C1_QueryCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	tracer := wlogpgx.Tracer(nil)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	const query = "SELECT id FROM orders WHERE token = 'hunter2' AND id = $1"
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: query, Args: []any{42}})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})
	end()

	record := onlyCall(t, rec)
	for key, want := range map[string]any{
		"kind": "db", "system": "postgresql", "operation": "SELECT", "status": "ok",
		"target": sqlshape.Shape(query, sqlshape.Unknown, 0),
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	if !conformance.Equal(record["rows"], int64(1)) {
		t.Errorf("calls[0].rows = %v, want 1", record["rows"])
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "hunter2") {
		t.Errorf("the secret reached the event: %s", body)
	}
}

// TestPgx_C1_QueryError proves that a failed query records the SQLSTATE and never the
// error text.
func TestPgx_C1_QueryError(t *testing.T) {
	log, rec := wlogtest.New(t)
	tracer := wlogpgx.Tracer(nil)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "INSERT INTO orders VALUES ($1)"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{
		Code: "23505", Message: "duplicate key value violates unique constraint",
	}})
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

// TestPgx_B2_BatchCall proves that one batch gives one call with the rows of every query.
func TestPgx_B2_BatchCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	tracer := wlogpgx.Tracer(nil)

	batchTracer := assertBatchTracer(t, tracer)
	batch := &pgx.Batch{}
	batch.Queue("INSERT INTO orders VALUES ($1)", 1)
	batch.Queue("INSERT INTO orders VALUES ($1)", 2)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx = batchTracer.TraceBatchStart(ctx, nil, pgx.TraceBatchStartData{Batch: batch})
	batchTracer.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{CommandTag: pgconn.NewCommandTag("INSERT 0 1")})
	batchTracer.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{CommandTag: pgconn.NewCommandTag("INSERT 0 1")})
	batchTracer.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})
	end()

	record := onlyCall(t, rec)
	if record["operation"] != "BATCH" {
		t.Errorf("calls[0].operation = %v, want BATCH", record["operation"])
	}
	if record["target"] != sqlshape.Shape("INSERT INTO orders VALUES ($1)", sqlshape.Unknown, 0) {
		t.Errorf("calls[0].target = %v, want the shape of the first query", record["target"])
	}
	if !conformance.Equal(record["rows"], int64(2)) {
		t.Errorf("calls[0].rows = %v, want 2", record["rows"])
	}
}

// TestPgx_B2_CopyCall proves that one copy gives one call named after its table, with the
// rows of the command tag.
func TestPgx_B2_CopyCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	tracer := wlogpgx.Tracer(nil)

	copyTracer := assertCopyTracer(t, tracer)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx = copyTracer.TraceCopyFromStart(ctx, nil, pgx.TraceCopyFromStartData{TableName: pgx.Identifier{"public", "orders"}})
	copyTracer.TraceCopyFromEnd(ctx, nil, pgx.TraceCopyFromEndData{CommandTag: pgconn.NewCommandTag("COPY 5")})
	end()

	record := onlyCall(t, rec)
	if record["operation"] != "COPY" {
		t.Errorf("calls[0].operation = %v, want COPY", record["operation"])
	}
	if record["target"] != "public.orders" {
		t.Errorf("calls[0].target = %v, want public.orders", record["target"])
	}
	if !conformance.Equal(record["rows"], int64(5)) {
		t.Errorf("calls[0].rows = %v, want 5", record["rows"])
	}
}

// TestPgx_B2_Forwards proves that every optional interface of the wrapped tracer keeps
// its calls.
func TestPgx_B2_Forwards(t *testing.T) {
	log, _ := wlogtest.New(t)
	next := &recordingTracer{}
	tracer := wlogpgx.Tracer(next)

	batchTracer := assertBatchTracer(t, tracer)
	copyTracer := assertCopyTracer(t, tracer)
	prepareTracer := assertPrepareTracer(t, tracer)
	batch := &pgx.Batch{}
	batch.Queue("SELECT 1")

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	ctx = batchTracer.TraceBatchStart(ctx, nil, pgx.TraceBatchStartData{Batch: batch})
	batchTracer.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{})
	batchTracer.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})
	ctx = copyTracer.TraceCopyFromStart(ctx, nil, pgx.TraceCopyFromStartData{TableName: pgx.Identifier{"orders"}})
	copyTracer.TraceCopyFromEnd(ctx, nil, pgx.TraceCopyFromEndData{})
	ctx = prepareTracer.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{Name: "s1", SQL: "SELECT 1"})
	prepareTracer.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})
	end()

	for name, got := range map[string]int{
		"query start": next.queryStarts, "query end": next.queryEnds,
		"batch start": next.batchStarts, "batch query": next.batchQueries, "batch end": next.batchEnds,
		"copy start": next.copyStarts, "copy end": next.copyEnds,
		"prepare start": next.prepareStarts, "prepare end": next.prepareEnds,
	} {
		if got != 1 {
			t.Errorf("the wrapped tracer saw %d %s calls, want 1", got, name)
		}
	}
}

// TestPgx_B2_PrepareIsNotACall proves that a prepare records nothing.
func TestPgx_B2_PrepareIsNotACall(t *testing.T) {
	log, rec := wlogtest.New(t)
	prepareTracer := assertPrepareTracer(t, wlogpgx.Tracer(nil))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx = prepareTracer.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{Name: "s1", SQL: "SELECT 1"})
	prepareTracer.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})
	end()

	if _, present := rec.Last()["calls"]; present {
		t.Errorf("a prepare recorded a call: %v", rec.Last()["calls"])
	}
}

// TestPgx_B2_NoEvent proves that a call outside a unit of work records nothing and
// reports nothing.
func TestPgx_B2_NoEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	tracer := wlogpgx.Tracer(nil)

	ctx := rec.Logger().WithContext(context.Background())
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	if count := len(rec.Events()); count != 0 {
		t.Errorf("events = %d, want none", count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("problems = %d, want none", count)
	}
}

// assertBatchTracer proves that the tracer carries the batch interface.
func assertBatchTracer(t *testing.T, tracer pgx.QueryTracer) pgx.BatchTracer {
	t.Helper()
	batchTracer, ok := tracer.(pgx.BatchTracer)
	if !ok {
		t.Fatal("the tracer has no BatchTracer interface")
	}
	return batchTracer
}

// assertCopyTracer proves that the tracer carries the copy interface.
func assertCopyTracer(t *testing.T, tracer pgx.QueryTracer) pgx.CopyFromTracer {
	t.Helper()
	copyTracer, ok := tracer.(pgx.CopyFromTracer)
	if !ok {
		t.Fatal("the tracer has no CopyFromTracer interface")
	}
	return copyTracer
}

// assertPrepareTracer proves that the tracer carries the prepare interface.
func assertPrepareTracer(t *testing.T, tracer pgx.QueryTracer) pgx.PrepareTracer {
	t.Helper()
	prepareTracer, ok := tracer.(pgx.PrepareTracer)
	if !ok {
		t.Fatal("the tracer has no PrepareTracer interface")
	}
	return prepareTracer
}

// recordingTracer is a pgx tracer that counts every call it receives.
type recordingTracer struct {
	queryStarts, queryEnds     int
	batchStarts, batchQueries  int
	batchEnds                  int
	copyStarts, copyEnds       int
	prepareStarts, prepareEnds int
}

// TraceQueryStart counts one query start.
func (r *recordingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	r.queryStarts++
	return ctx
}

// TraceQueryEnd counts one query end.
func (r *recordingTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
	r.queryEnds++
}

// TraceBatchStart counts one batch start.
func (r *recordingTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	r.batchStarts++
	return ctx
}

// TraceBatchQuery counts one batch query.
func (r *recordingTracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {
	r.batchQueries++
}

// TraceBatchEnd counts one batch end.
func (r *recordingTracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {
	r.batchEnds++
}

// TraceCopyFromStart counts one copy start.
func (r *recordingTracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceCopyFromStartData) context.Context {
	r.copyStarts++
	return ctx
}

// TraceCopyFromEnd counts one copy end.
func (r *recordingTracer) TraceCopyFromEnd(context.Context, *pgx.Conn, pgx.TraceCopyFromEndData) {
	r.copyEnds++
}

// TracePrepareStart counts one prepare start.
func (r *recordingTracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, _ pgx.TracePrepareStartData) context.Context {
	r.prepareStarts++
	return ctx
}

// TracePrepareEnd counts one prepare end.
func (r *recordingTracer) TracePrepareEnd(context.Context, *pgx.Conn, pgx.TracePrepareEndData) {
	r.prepareEnds++
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
