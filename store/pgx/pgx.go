// Package wlogpgx adapts the work kit to pgx, so one query, batch, or copy records one
// call on the open event, with the statement shape and no argument.
//
// Read top to bottom: Tracer returns the pgx.QueryTracer that records every call and
// forwards every optional interface of the tracer it wraps. TraceQueryStart opens the
// call of one query, TraceBatchStart opens one call for a whole batch and counts its
// rows, and TraceCopyFromStart names the table of a copy. A prepare is not a call,
// because pgx caches a prepared statement across requests.
//
// This is the whole setup:
//
//	cfg, _ := pgx.ParseConfig(dsn)
//	cfg.Tracer = wlogpgx.Tracer(cfg.Tracer)
//	conn, _ := pgx.ConnectConfig(ctx, cfg)
package wlogpgx

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/store/sqlshape"
)

// system names the database of every record, because pgx speaks to Postgres only.
const system = "postgresql"

// Tracer returns the pgx tracer that records one call per query, batch, and copy, and
// forwards every optional interface of next. A nil next records without forwarding.
func Tracer(next pgx.QueryTracer) pgx.QueryTracer {
	return &tracer{next: next}
}

// tracer records pgx calls and forwards to the tracer of the app.
type tracer struct {
	next pgx.QueryTracer
}

// stateKey is the context key that carries the state of one running call.
type stateKey struct{}

// callState holds the end function and the row count of one running call.
type callState struct {
	end  func(wlog.CallResult)
	rows int64
	done bool
}

// TraceQueryStart opens the call of one query.
func (t *tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	shape := sqlshape.Shape(data.SQL, sqlshape.Unknown, 0)
	ctx, state := begin(ctx, wlog.Call{
		Kind: "db", System: system, Operation: sqlshape.Operation(shape), Target: shape,
	})
	ctx = context.WithValue(ctx, stateKey{}, state)
	if t.next != nil {
		ctx = t.next.TraceQueryStart(ctx, conn, data)
	}
	return ctx
}

// TraceQueryEnd ends the call of one query, with the row count of the command tag.
func (t *tracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if t.next != nil {
		t.next.TraceQueryEnd(ctx, conn, data)
	}
	finish(ctx, data.CommandTag.RowsAffected(), data.Err)
}

// TraceBatchStart opens one call for a whole batch, with the shape of its first query.
func (t *tracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	shape := ""
	if data.Batch != nil && len(data.Batch.QueuedQueries) > 0 {
		shape = sqlshape.Shape(data.Batch.QueuedQueries[0].SQL, sqlshape.Unknown, 0)
	}
	ctx, state := begin(ctx, wlog.Call{
		Kind: "db", System: system, Operation: "BATCH", Target: shape,
	})
	ctx = context.WithValue(ctx, stateKey{}, state)
	if next, ok := t.next.(pgx.BatchTracer); ok {
		ctx = next.TraceBatchStart(ctx, conn, data)
	}
	return ctx
}

// TraceBatchQuery counts the rows of one query of the batch.
func (t *tracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	if next, ok := t.next.(pgx.BatchTracer); ok {
		next.TraceBatchQuery(ctx, conn, data)
	}
	if state := stateOf(ctx); state != nil {
		state.rows += data.CommandTag.RowsAffected()
	}
}

// TraceBatchEnd ends the call of the batch.
func (t *tracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	if next, ok := t.next.(pgx.BatchTracer); ok {
		next.TraceBatchEnd(ctx, conn, data)
	}
	finish(ctx, 0, data.Err)
}

// TraceCopyFromStart opens one copy call, named after its table.
func (t *tracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	ctx, state := begin(ctx, wlog.Call{
		Kind: "db", System: system, Operation: "COPY", Target: strings.Join(data.TableName, "."),
	})
	ctx = context.WithValue(ctx, stateKey{}, state)
	if next, ok := t.next.(pgx.CopyFromTracer); ok {
		ctx = next.TraceCopyFromStart(ctx, conn, data)
	}
	return ctx
}

// TraceCopyFromEnd ends the call of one copy, with the row count of the command tag.
func (t *tracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	if next, ok := t.next.(pgx.CopyFromTracer); ok {
		next.TraceCopyFromEnd(ctx, conn, data)
	}
	finish(ctx, data.CommandTag.RowsAffected(), data.Err)
}

// TracePrepareStart forwards the start of a prepare to the tracer of the app. A prepare
// is not a call by default, because pgx caches a prepared statement across requests.
func (t *tracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	if next, ok := t.next.(pgx.PrepareTracer); ok {
		return next.TracePrepareStart(ctx, conn, data)
	}
	return ctx
}

// TracePrepareEnd forwards the end of a prepare to the tracer of the app.
func (t *tracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	if next, ok := t.next.(pgx.PrepareTracer); ok {
		next.TracePrepareEnd(ctx, conn, data)
	}
}

// begin opens one db call and returns its state.
func begin(ctx context.Context, call wlog.Call) (context.Context, *callState) {
	ctx, end := wlog.StartCall(ctx, call)
	return ctx, &callState{end: end}
}

// stateOf returns the state of the running call of one context, or nil.
func stateOf(ctx context.Context) *callState {
	state, _ := ctx.Value(stateKey{}).(*callState)
	return state
}

// finish ends one call with a row count and the error of the driver. A second finish of
// the same call does nothing.
func finish(ctx context.Context, rows int64, err error) {
	state := stateOf(ctx)
	if state == nil || state.done {
		return
	}
	state.done = true
	if err != nil {
		state.end(wlog.CallResult{Rows: state.rows + rows, Err: err, ErrCode: errorCode(err)})
		return
	}
	state.end(wlog.CallResult{Status: "ok", Rows: state.rows + rows})
}

// errorCode returns the code of one driver error, and it never holds the error text.
func errorCode(err error) string {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		if code := state.SQLState(); code != "" {
			return code
		}
	}
	return "error"
}
