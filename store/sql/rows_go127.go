//go:build go1.27

// This file holds the row wrapper of Go 1.27 and later. database/sql scans a row through
// RowsColumnScanner when the driver has it, so the wrapper carries the interface only for
// a driver that does too.
package wlogsql

import (
	"database/sql/driver"

	"github.com/jeremygprawira/wlog"
)

// wrapRows wraps one driver.Rows. A driver with RowsColumnScanner gets the scanner
// wrapper, whose ScanColumn reaches the driver, and any other driver gets the plain
// wrapper.
func wrapRows(inner driver.Rows, end func(wlog.CallResult)) driver.Rows {
	base := &rows{inner: inner, end: end}
	scanner, ok := inner.(driver.RowsColumnScanner)
	if !ok {
		return base
	}
	return &rowsScanner{rows: base, scanner: scanner}
}

// rowsScanner carries RowsColumnScanner of the driver.
type rowsScanner struct {
	*rows
	scanner driver.RowsColumnScanner
}

// NextRow advances one row, and it counts the row the driver wrote.
func (r *rowsScanner) NextRow() error {
	err := r.scanner.NextRow()
	if err == nil {
		r.count++
	}
	return err
}

// ScanColumn scans one column of the current row through the driver.
func (r *rowsScanner) ScanColumn(scanCtx driver.ScanContext, index int, dest any) error {
	return r.scanner.ScanColumn(scanCtx, index, dest)
}
