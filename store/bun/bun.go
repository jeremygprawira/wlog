// Package wlogbun adapts the work kit to bun, so one query records one call on the open
// event.
//
// Read top to bottom: Hook implements bun.QueryHook. BeforeQuery opens one call with the
// statement shape, and AfterQuery ends it with the affected rows and the error. The query
// text of bun holds the argument values, so the hook always passes it through sqlshape.
//
// This is the whole setup:
//
//	db := bun.NewDB(sqldb, sqlitedialect.New())
//	db.AddQueryHook(wlogbun.Hook())
package wlogbun

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/store/sqlshape"
)

// Hook returns the bun query hook that records one call per query. Install it with
// db.AddQueryHook.
func Hook() bun.QueryHook { return hook{} }

// hook records one call per query.
type hook struct{}

// endKey is the context key that carries the end function of one running call.
type endKey struct{}

// BeforeQuery opens one call with the statement shape, and it keeps the end function on
// the context for AfterQuery.
func (hook) BeforeQuery(ctx context.Context, evt *bun.QueryEvent) context.Context {
	shape := sqlshape.Shape(evt.Query, sqlshape.Unknown, 0)
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "db", System: evt.DB.Dialect().Name().String(),
		Operation: sqlshape.Operation(shape), Target: shape,
	})
	return context.WithValue(ctx, endKey{}, end)
}

// AfterQuery ends the call with the affected rows and the error. A query that found no
// row is an answer and not a failure, which is how bun counts it too.
func (hook) AfterQuery(ctx context.Context, evt *bun.QueryEvent) {
	end, ok := ctx.Value(endKey{}).(func(wlog.CallResult))
	if !ok {
		return
	}
	var rows int64
	if evt.Result != nil {
		rows, _ = evt.Result.RowsAffected()
	}
	result := wlog.CallResult{Status: "ok", Rows: rows}
	if evt.Err != nil && !errors.Is(evt.Err, sql.ErrNoRows) {
		result.Status = ""
		result.Err = evt.Err
		result.ErrCode = errorCode(evt.Err)
	}
	end(result)
}

// errorCode returns the code of one driver error, such as a SQLSTATE, and it never holds
// the error text.
func errorCode(err error) string {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		if code := state.SQLState(); code != "" {
			return code
		}
	}
	return "error"
}
