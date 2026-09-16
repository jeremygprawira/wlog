//go:build !go1.23

// The !go1.23 build tag selects this file on a toolchain older than Go 1.23,
// where net/http.Request has no Pattern field. The route then falls back to the
// request path.
package wlogstd

import "net/http"

// requestPattern returns "" on a toolchain older than Go 1.23, so the route
// falls back to the request path.
func requestPattern(r *http.Request) string {
	return ""
}
