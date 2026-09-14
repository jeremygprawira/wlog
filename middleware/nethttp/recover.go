package wlogstd

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/jeremygprawira/wlog"
)

// panicError wraps a recovered panic value with the stack captured at the moment of
// recovery. Its optional Stack() method is picked up by core's default ErrorExtractor
// (and any other extractor that chooses to look for it).
type panicError struct {
	value any
	stack string
}

func (e *panicError) Error() string { return fmt.Sprintf("panic: %v", e.value) }
func (e *panicError) Stack() string { return e.stack }

// recoverPanic turns a recovered value into a 500 response (if nothing was written
// yet) and a wlog.Error with a stack trace, then lets the request finish normally —
// the process keeps serving.
func recoverPanic(ctx context.Context, sw *statusWriter) {
	if rec := recover(); rec != nil {
		if !sw.wroteHeader {
			sw.WriteHeader(http.StatusInternalServerError)
		}
		wlog.Error(ctx, &panicError{value: rec, stack: string(debug.Stack())})
	}
}
