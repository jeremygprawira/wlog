// This file reads the route template that net/http's own ServeMux matched.
package httpcore

import (
	"net/http"
	"reflect"
	"strings"
)

// requestPattern returns the route template that the router matched, and whether it
// matched one at all.
//
// The Pattern field of net/http.Request arrived in Go 1.23, and the root module keeps a
// Go 1.21 floor. A go1.23 build tag would not help here: the language version of go.mod
// gates that tag, so the field would stay unread in a module-mode build on a modern
// toolchain. The field is read by name instead, which works on every toolchain and costs
// one field lookup per request.
//
// net/http writes the pattern as "METHOD /path", so the method leaves the template here.
func requestPattern(r *http.Request) (string, bool) {
	field := reflect.ValueOf(r).Elem().FieldByName("Pattern")
	if field.Kind() != reflect.String {
		return "", false
	}
	pattern := field.String()
	if pattern == "" {
		return "", false
	}
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return path, true
	}
	return pattern, true
}
