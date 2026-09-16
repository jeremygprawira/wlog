//go:build go1.23

// The go1.23 build tag selects this file on a Go 1.23 or later toolchain, where
// net/http.Request carries the Pattern field that ServeMux sets.
package wlogstd

import "net/http"

// requestPattern returns the pattern that the router matched, or "" when the
// router set no pattern.
func requestPattern(r *http.Request) string {
	return r.Pattern
}
