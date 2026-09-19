// This file holds the options of the net/http adapter. The pipeline owns the behavior, so
// this package re-exports the http-core options under one import, and adds the default
// route reader.
package wlogstd

import (
	"net/http"
	"strings"

	"github.com/jeremygprawira/wlog/middleware/httpcore"
)

// Option is an http-core option, re-exported so a caller needs one import.
type Option = httpcore.Option

// The http-core options this adapter passes through.
var (
	SkipPaths      = httpcore.SkipPaths
	CaptureAll     = httpcore.CaptureAll
	CaptureBody    = httpcore.CaptureBody
	MaxBody        = httpcore.MaxBody
	BodyTypes      = httpcore.BodyTypes
	CaptureHeaders = httpcore.CaptureHeaders
	WithUserFunc   = httpcore.User
	WithRouteFunc  = httpcore.RouteFunc
)

// route returns the route template the router matched, and an empty string when no route
// matched. net/http writes the pattern as "METHOD /path", so the method leaves here.
//
// The Pattern field of net/http.Request carries the template, and it needs Go 1.23. The
// build tag selects the reader: r.Pattern on Go 1.23 and later, and an empty string
// before that. An empty route makes the operation {METHOD} unmatched, which is what an
// unmatched request deserves.
func route(r *http.Request) string {
	pattern := requestPattern(r)
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return path
	}
	return pattern
}
