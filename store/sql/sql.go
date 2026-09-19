// Package wlogsql records every database/sql call as one call record on the open event,
// with no raw SQL and no parameter values.
//
// Read top to bottom: Wrap and WrapDriver put the wrapper around a driver. The conn
// wrapper records one call per Exec and Query, the stmt wrapper records one call per
// prepared call, and the rows wrapper counts the rows and ends the call at Close.
//
// The wrapper keeps every optional interface of the driver. database/sql inspects those
// interfaces, so adding one the driver does not have changes behavior. A session resetter
// and a validator decide whether database/sql keeps a connection after a rollback. A
// column converter changes how arguments convert. A statement context method decides
// whether the driver sees the context of the call.
//
// This is the whole setup:
//
//	db := sql.OpenDB(wlogsql.Wrap(connector, wlogsql.WithSystem("postgresql")))
package wlogsql

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/store/sqlshape"
)

// Option configures a wrapper.
type Option func(*options)

// options holds the resolved settings of one wrapper.
type options struct {
	system string
}

// WithSystem names the database system of every call record, such as postgresql or mysql.
// The default is the type name of the wrapped driver.
func WithSystem(name string) Option {
	return func(o *options) { o.system = name }
}

// resolve returns the settings of one wrapper.
func resolve(opts []Option, d driver.Driver) options {
	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.system == "" {
		cfg.system = driverName(d)
	}
	return cfg
}

// driverName returns the type name of one driver, with no pointer mark, so a record shows
// stdlib.Driver rather than *stdlib.Driver.
func driverName(d driver.Driver) string {
	if d == nil {
		return ""
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", d), "*")
}

// Wrap returns a connector that records one call for every Exec and Query of the
// connections it hands out. Pass it to sql.OpenDB.
func Wrap(c driver.Connector, opts ...Option) driver.Connector {
	return &connector{inner: c, cfg: resolve(opts, c.Driver())}
}

// connector wraps one driver.Connector.
type connector struct {
	inner driver.Connector
	cfg   options
}

// Connect returns the wrapped connection.
func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return wrapConn(conn, c.cfg), nil
}

// Driver returns the driver of the wrapped connector.
func (c *connector) Driver() driver.Driver { return c.inner.Driver() }

// WrapDriver returns a driver that records one call for every Exec and Query. Register
// the result with sql.Register under a name of its own.
func WrapDriver(d driver.Driver, opts ...Option) driver.Driver {
	cfg := resolve(opts, d)
	if dc, ok := d.(driver.DriverContext); ok {
		return &driverContext{inner: dc, cfg: cfg}
	}
	return &plainDriver{inner: d, cfg: cfg}
}

// driverContext keeps the DriverContext interface of the wrapped driver, because
// database/sql prefers it over a plain Open.
type driverContext struct {
	inner driver.DriverContext
	cfg   options
}

// OpenConnector returns the wrapped connector.
func (d *driverContext) OpenConnector(name string) (driver.Connector, error) {
	c, err := d.inner.OpenConnector(name)
	if err != nil {
		return nil, err
	}
	return &connector{inner: c, cfg: d.cfg}, nil
}

// Open opens one connection through the connector of the driver. database/sql uses
// OpenConnector for this driver, and Open exists so the wrapper stays a driver.Driver.
func (d *driverContext) Open(name string) (driver.Conn, error) {
	c, err := d.inner.OpenConnector(name)
	if err != nil {
		return nil, err
	}
	conn, err := c.Connect(context.Background())
	if err != nil {
		return nil, err
	}
	return wrapConn(conn, d.cfg), nil
}

// plainDriver wraps a driver that has no DriverContext.
type plainDriver struct {
	inner driver.Driver
	cfg   options
}

// Open returns the wrapped connection.
func (d *plainDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return wrapConn(conn, d.cfg), nil
}

// conn holds the state of every connection wrapper.
type conn struct {
	inner driver.Conn
	cfg   options
}

// wrapConn returns a connection wrapper whose optional interfaces match the driver.
//
// database/sql keeps a connection after a rollback only when the connection has both a
// session resetter and a validator. A wrapper that always has both would change that
// decision for a driver that has neither, so the wrapper type follows the driver.
func wrapConn(inner driver.Conn, cfg options) driver.Conn {
	core := &conn{inner: inner, cfg: cfg}
	_, reset := inner.(driver.SessionResetter)
	_, valid := inner.(driver.Validator)
	switch {
	case reset && valid:
		return &connBoth{conn: core}
	case reset:
		return &connResetter{conn: core}
	case valid:
		return &connValidator{conn: core}
	}
	return core
}

// connResetter adds the session reset of the driver.
type connResetter struct{ *conn }

// ResetSession resets the session of the driver.
func (c *connResetter) ResetSession(ctx context.Context) error {
	return c.inner.(driver.SessionResetter).ResetSession(ctx)
}

// connValidator adds the validity check of the driver.
type connValidator struct{ *conn }

// IsValid reports whether the connection is still usable.
func (c *connValidator) IsValid() bool { return c.inner.(driver.Validator).IsValid() }

// connBoth adds both the session reset and the validity check of the driver.
type connBoth struct{ *conn }

// ResetSession resets the session of the driver.
func (c *connBoth) ResetSession(ctx context.Context) error {
	return c.inner.(driver.SessionResetter).ResetSession(ctx)
}

// IsValid reports whether the connection is still usable.
func (c *connBoth) IsValid() bool { return c.inner.(driver.Validator).IsValid() }

// Prepare prepares one statement. Preparing is not a call by itself, because a lazy
// re-prepare runs under the context of a later request.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.inner.Prepare(query)
	return wrapStmt(c, query, stmt, err)
}

// PrepareContext prepares one statement, and it copies the post-Prepare context check of
// database/sql when the driver has no PrepareContext.
func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if inner, ok := c.inner.(driver.ConnPrepareContext); ok {
		stmt, err := inner.PrepareContext(ctx, query)
		return wrapStmt(c, query, stmt, err)
	}
	stmt, err := c.inner.Prepare(query)
	if err == nil {
		select {
		case <-ctx.Done():
			_ = stmt.Close()
			return nil, ctx.Err()
		default:
		}
	}
	return wrapStmt(c, query, stmt, err)
}

// Close closes the connection.
func (c *conn) Close() error { return c.inner.Close() }

// Begin starts one transaction.
func (c *conn) Begin() (driver.Tx, error) {
	return c.inner.Begin() //nolint:staticcheck // a driver without ConnBeginTx has only Begin
}

// BeginTx starts one transaction, and it copies the checks of database/sql when the driver
// has no ConnBeginTx.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if inner, ok := c.inner.(driver.ConnBeginTx); ok {
		return inner.BeginTx(ctx, opts)
	}
	if opts.Isolation != driver.IsolationLevel(0) {
		return nil, errors.New("sql: driver does not support non-default isolation level")
	}
	if opts.ReadOnly {
		return nil, errors.New("sql: driver does not support read-only transactions")
	}
	if ctx.Done() == nil {
		return c.inner.Begin() //nolint:staticcheck // a driver without ConnBeginTx has only Begin
	}
	tx, err := c.inner.Begin() //nolint:staticcheck // a driver without ConnBeginTx has only Begin
	if err == nil {
		select {
		case <-ctx.Done():
			_ = tx.Rollback()
			return nil, ctx.Err()
		default:
		}
	}
	return tx, err
}

// Ping pings the connection when the driver can, and reports success otherwise, as
// database/sql does with no pinger.
func (c *conn) Ping(ctx context.Context) error {
	if inner, ok := c.inner.(driver.Pinger); ok {
		return inner.Ping(ctx)
	}
	return nil
}

// CheckNamedValue accepts one argument through the driver checker, and it reports
// driver.ErrSkip when the driver has none, so database/sql keeps its own conversion.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if inner, ok := c.inner.(driver.NamedValueChecker); ok {
		return inner.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// Raw returns the connection of the driver, so code that reads the raw connection keeps
// working under the wrapper.
func (c *conn) Raw() driver.Conn { return c.inner }

// ExecContext runs one statement, and it returns driver.ErrSkip as the exact sentinel
// when the driver does not run it.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	inner, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	ctx, end := beginCall(ctx, c.cfg, query)
	res, err := inner.ExecContext(ctx, query, args)
	if err == driver.ErrSkip { //nolint:errorlint // database/sql compares the exact sentinel
		// database/sql prepares the statement instead, and that call records the
		// work, so this skipped attempt records nothing.
		return nil, err
	}
	endExec(end, res, err)
	return res, err
}

// QueryContext runs one query, and it returns driver.ErrSkip as the exact sentinel when
// the driver does not run it. The call ends at Rows.Close.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	inner, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	ctx, end := beginCall(ctx, c.cfg, query)
	rows, err := inner.QueryContext(ctx, query, args)
	if err == driver.ErrSkip { //nolint:errorlint // database/sql compares the exact sentinel
		return nil, err
	}
	if err != nil {
		end(wlog.CallResult{Err: err, ErrCode: errorCode(err)})
		return nil, err
	}
	return wrapRows(rows, end), nil
}

// stmt wraps one prepared statement.
//
// The wrapper always carries ExecContext and QueryContext, because database/sql uses them
// when they exist and never falls back. When the driver has no statement context method,
// the wrapper copies the conversion and the context check of database/sql.
type stmt struct {
	inner driver.Stmt
	query string
	conn  *conn
}

// wrapStmt wraps one prepared statement. The wrapper carries the ColumnConverter of the
// driver only when the driver has it, because that interface changes how arguments
// convert.
func wrapStmt(c *conn, query string, s driver.Stmt, err error) (driver.Stmt, error) {
	if err != nil {
		return nil, err
	}
	base := &stmt{inner: s, query: query, conn: c}
	if converter, ok := s.(driver.ColumnConverter); ok { //nolint:staticcheck // the wrapper keeps the deprecated interface of the driver
		return &stmtConvert{stmt: base, inner: converter}, nil
	}
	return base, nil
}

// stmtConvert carries the column converter of the driver.
type stmtConvert struct {
	*stmt
	inner driver.ColumnConverter //nolint:staticcheck // the wrapper keeps the deprecated interface of the driver
}

// ColumnConverter returns the converter of the driver for one column.
func (s *stmtConvert) ColumnConverter(index int) driver.ValueConverter {
	return s.inner.ColumnConverter(index)
}

// Close closes the statement.
func (s *stmt) Close() error { return s.inner.Close() }

// NumInput returns the number of placeholders of the statement, and -1 when the driver
// does not know.
func (s *stmt) NumInput() int { return s.inner.NumInput() }

// Exec runs one statement without the context of the call.
func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	_, end := beginCall(context.Background(), s.conn.cfg, s.query)
	res, err := s.inner.Exec(args) //nolint:staticcheck // a driver without StmtExecContext has only Exec
	endExec(end, res, err)
	return res, err
}

// Query runs one query without the context of the call. The call ends at Rows.Close.
func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	_, end := beginCall(context.Background(), s.conn.cfg, s.query)
	rows, err := s.inner.Query(args) //nolint:staticcheck // a driver without StmtQueryContext has only Query
	if err != nil {
		end(wlog.CallResult{Err: err, ErrCode: errorCode(err)})
		return nil, err
	}
	return wrapRows(rows, end), nil
}

// ExecContext runs one statement with the context of the call.
func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	inner, ok := s.inner.(driver.StmtExecContext)
	if !ok {
		values, err := namedValues(args)
		if err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		_, end := beginCall(ctx, s.conn.cfg, s.query)
		res, err := s.inner.Exec(values) //nolint:staticcheck // a driver without StmtExecContext has only Exec
		endExec(end, res, err)
		return res, err
	}
	ctx, end := beginCall(ctx, s.conn.cfg, s.query)
	res, err := inner.ExecContext(ctx, args)
	endExec(end, res, err)
	return res, err
}

// QueryContext runs one query with the context of the call. The call ends at Rows.Close.
func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	inner, ok := s.inner.(driver.StmtQueryContext)
	if !ok {
		values, err := namedValues(args)
		if err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		_, end := beginCall(ctx, s.conn.cfg, s.query)
		rows, err := s.inner.Query(values) //nolint:staticcheck // a driver without StmtQueryContext has only Query
		if err != nil {
			end(wlog.CallResult{Err: err, ErrCode: errorCode(err)})
			return nil, err
		}
		return wrapRows(rows, end), nil
	}
	ctx, end := beginCall(ctx, s.conn.cfg, s.query)
	rows, err := inner.QueryContext(ctx, args)
	if err != nil {
		end(wlog.CallResult{Err: err, ErrCode: errorCode(err)})
		return nil, err
	}
	return wrapRows(rows, end), nil
}

// CheckNamedValue accepts one argument through the checker of the statement, then the
// checker of the connection, and it reports driver.ErrSkip when neither accepts it.
//
// database/sql reads only one checker, the statement's or the connection's, so a wrapper
// statement that answers for itself would hide the connection checker.
func (s *stmt) CheckNamedValue(nv *driver.NamedValue) error {
	if inner, ok := s.inner.(driver.NamedValueChecker); ok {
		if err := inner.CheckNamedValue(nv); err != driver.ErrSkip { //nolint:errorlint // the checker reports the exact sentinel
			return err
		}
	}
	if inner, ok := s.conn.inner.(driver.NamedValueChecker); ok {
		if err := inner.CheckNamedValue(nv); err != driver.ErrSkip { //nolint:errorlint // the checker reports the exact sentinel
			return err
		}
	}
	return driver.ErrSkip
}

// namedValues turns named arguments into plain values, and it copies the error of
// database/sql for a driver that takes no named argument.
func namedValues(args []driver.NamedValue) ([]driver.Value, error) {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		if arg.Name != "" {
			return nil, errors.New("sql: driver does not support the use of Named Parameters")
		}
		values[i] = arg.Value
	}
	return values, nil
}

// rows wraps one driver.Rows, counts the rows it reads, and ends the call at Close.
type rows struct {
	inner driver.Rows
	end   func(wlog.CallResult)
	count int64
	done  bool
}

// Columns returns the column names.
func (r *rows) Columns() []string { return r.inner.Columns() }

// Close ends the call and closes the rows. It reports the count of the rows the caller
// read, and the error of the driver.
func (r *rows) Close() error {
	err := r.inner.Close()
	r.finish(err)
	return err
}

// Next reads one row, and it counts the row the driver wrote.
func (r *rows) Next(dest []driver.Value) error {
	err := r.inner.Next(dest)
	if err == nil {
		r.count++
	}
	return err
}

// finish ends the call once.
func (r *rows) finish(err error) {
	if r.done {
		return
	}
	r.done = true
	if err != nil {
		r.end(wlog.CallResult{Rows: r.count, Err: err, ErrCode: errorCode(err)})
		return
	}
	r.end(wlog.CallResult{Status: "ok", Rows: r.count})
}

// ColumnTypeDatabaseTypeName returns the database type name of one column when the driver
// reports it.
func (r *rows) ColumnTypeDatabaseTypeName(index int) string {
	if inner, ok := r.inner.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return inner.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

// ColumnTypeLength returns the length of one column when the driver reports it.
func (r *rows) ColumnTypeLength(index int) (length int64, ok bool) {
	if inner, ok := r.inner.(driver.RowsColumnTypeLength); ok {
		return inner.ColumnTypeLength(index)
	}
	return 0, false
}

// ColumnTypeNullable reports whether one column can be null, when the driver knows.
func (r *rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	if inner, ok := r.inner.(driver.RowsColumnTypeNullable); ok {
		return inner.ColumnTypeNullable(index)
	}
	return false, false
}

// ColumnTypePrecisionScale returns the precision and the scale of one column when the
// driver reports them.
func (r *rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	if inner, ok := r.inner.(driver.RowsColumnTypePrecisionScale); ok {
		return inner.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

// ColumnTypeScanType returns the scan type of one column, exactly as the driver reports
// it, and nil when the driver does not report one.
func (r *rows) ColumnTypeScanType(index int) reflect.Type {
	if inner, ok := r.inner.(driver.RowsColumnTypeScanType); ok {
		return inner.ColumnTypeScanType(index)
	}
	return nil
}

// beginCall opens one recorded call for one statement.
func beginCall(ctx context.Context, cfg options, query string) (context.Context, func(wlog.CallResult)) {
	return wlog.StartCall(ctx, callOf(cfg, query))
}

// callOf describes one statement for the calls list. The target holds the statement
// shape, so a record never holds raw SQL.
func callOf(cfg options, query string) wlog.Call {
	shape := sqlshape.Shape(query, sqlshape.Unknown, 0)
	return wlog.Call{
		Kind:      "db",
		System:    cfg.system,
		Operation: sqlshape.Operation(shape),
		Target:    shape,
	}
}

// endExec ends one exec call with the result of the driver.
func endExec(end func(wlog.CallResult), res driver.Result, err error) {
	if err != nil {
		end(wlog.CallResult{Err: err, ErrCode: errorCode(err)})
		return
	}
	var rows int64
	if res != nil {
		rows, _ = res.RowsAffected()
	}
	end(wlog.CallResult{Status: "ok", Rows: rows})
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
