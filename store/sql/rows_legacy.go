//go:build !go1.27

// This file holds the row wrapper of the Go releases before 1.27. Those releases scan a
// row through Rows.Next, so the plain wrapper is the whole wrapper.
package wlogsql

import (
	"database/sql/driver"

	"github.com/jeremygprawira/wlog"
)

// wrapRows wraps one driver.Rows.
func wrapRows(inner driver.Rows, end func(wlog.CallResult)) driver.Rows {
	return &rows{inner: inner, end: end}
}
