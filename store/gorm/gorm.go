// Package wloggorm adapts the work kit to gorm, so one Create, Query, Update, Delete,
// Row, or Raw call records one call on the open event.
//
// Read top to bottom: Plugin registers a pair of callbacks around every gorm callback,
// named wlog:*. The before callback opens one call with the operation and the table of
// the statement, and the after callback ends it with the shape of the built SQL, the
// affected rows, and the error. A record that gorm marks not found is not an error.
//
// This is the whole setup:
//
//	db, _ := gorm.Open(dialector, &gorm.Config{})
//	_ = db.Use(wloggorm.Plugin())
package wloggorm

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/store/sqlshape"
)

// Plugin returns the gorm plugin that records one call per operation. Install it with
// db.Use(wloggorm.Plugin()).
func Plugin() gorm.Plugin { return plugin{} }

// plugin registers the callback pairs of every gorm operation.
type plugin struct{}

// Name names the plugin.
func (plugin) Name() string { return "wlog" }

// Initialize registers one pair of wlog callbacks around every gorm callback.
func (plugin) Initialize(db *gorm.DB) error {
	create := db.Callback().Create()
	if err := create.Before("gorm:create").Register("wlog:before_create", before("INSERT")); err != nil {
		return err
	}
	if err := create.After("gorm:create").Register("wlog:after_create", finish); err != nil {
		return err
	}

	query := db.Callback().Query()
	if err := query.Before("gorm:query").Register("wlog:before_query", before("SELECT")); err != nil {
		return err
	}
	if err := query.After("gorm:query").Register("wlog:after_query", finish); err != nil {
		return err
	}

	update := db.Callback().Update()
	if err := update.Before("gorm:update").Register("wlog:before_update", before("UPDATE")); err != nil {
		return err
	}
	if err := update.After("gorm:update").Register("wlog:after_update", finish); err != nil {
		return err
	}

	deleteProc := db.Callback().Delete()
	if err := deleteProc.Before("gorm:delete").Register("wlog:before_delete", before("DELETE")); err != nil {
		return err
	}
	if err := deleteProc.After("gorm:delete").Register("wlog:after_delete", finish); err != nil {
		return err
	}

	row := db.Callback().Row()
	if err := row.Before("gorm:row").Register("wlog:before_row", before("SELECT")); err != nil {
		return err
	}
	if err := row.After("gorm:row").Register("wlog:after_row", finish); err != nil {
		return err
	}

	raw := db.Callback().Raw()
	if err := raw.Before("gorm:raw").Register("wlog:before_raw", before("RAW")); err != nil {
		return err
	}
	if err := raw.After("gorm:raw").Register("wlog:after_raw", finish); err != nil {
		return err
	}
	return nil
}

// endKey is the context key that carries the end function of one running call.
type endKey struct{}

// before opens one db call for one operation. The statement carries the table and the
// context, and the shape arrives with the after callback, because gorm builds the SQL
// inside its own callback.
func before(operation string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		stmt := db.Statement
		if stmt == nil {
			return
		}
		ctx, end := wlog.StartCall(stmt.Context, wlog.Call{
			Kind:      "db",
			System:    db.Name(),
			Operation: operation,
			Target:    stmt.Table,
		})
		stmt.Context = context.WithValue(ctx, endKey{}, end)
	}
}

// finish ends one call with the shape, the rows, and the error of the statement.
func finish(db *gorm.DB) {
	stmt := db.Statement
	if stmt == nil || stmt.Context == nil {
		return
	}
	end, ok := stmt.Context.Value(endKey{}).(func(wlog.CallResult))
	if !ok {
		return
	}
	result := wlog.CallResult{
		Status: "ok",
		Rows:   db.RowsAffected,
		Attrs:  map[string]any{"shape": sqlshape.Shape(stmt.SQL.String(), sqlshape.Unknown, 0)},
	}
	if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
		result.Status = ""
		result.Err = db.Error
		result.ErrCode = errorCode(db.Error)
	}
	end(result)
}

// errorCode returns the code of one error, such as a SQLSTATE, and it never holds the
// error text.
func errorCode(err error) string {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		if code := state.SQLState(); code != "" {
			return code
		}
	}
	return "error"
}
