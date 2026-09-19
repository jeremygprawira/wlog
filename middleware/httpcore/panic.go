// This file holds the panic policy: what the middleware does when the handler chain
// panics, so a panic still emits the event it belongs to.
package httpcore

import (
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
)

// Policy is what the middleware does with a panic in the handler chain.
type Policy int

const (
	// Recover500 records the panic, writes a 500 when nothing was written yet, and emits
	// the event. The handler chain stops, and the caller sees the response.
	Recover500 Policy = iota
	// Repanic records the panic and emits the event, and then lets the panic continue.
	Repanic
)

// PanicPolicy sets the panic policy. The default is Recover500. http.ErrAbortHandler
// always continues, whatever the policy says, because net/http reads it.
func PanicPolicy(p Policy) Option { return func(c *config) { c.panicPolicy = p } }

// panicError carries a recovered panic value and its stack, which the default extractor
// reads through the Stack method.
type panicError struct {
	value any
	stack string
}

// isAbortHandler reports whether a panic value is http.ErrAbortHandler, which net/http
// reads to cut the connection without a log line. A wrapper around it counts too.
func isAbortHandler(value any) bool {
	err, ok := value.(error)
	return ok && errors.Is(err, http.ErrAbortHandler)
}

// Error returns the panic value as a message.
func (e *panicError) Error() string { return fmt.Sprintf("panic: %v", e.value) }

// Stack returns the stack of the moment the panic was recovered.
func (e *panicError) Stack() string { return e.stack }

// runHandler calls the handler chain under the panic policy, and returns the value of a
// panic that must continue after the event emits.
//
// A panic is recorded on the event with its stack. With Recover500 and nothing written
// yet, the wrapper writes a 500, so the response and the event agree.
func (c *Core) runHandler(x *Exchange, sw *statusWriter, r *http.Request, next http.Handler) (again any) {
	defer func() {
		value := recover()
		if value == nil {
			return
		}
		x.Panic(value, debug.Stack())
		if isAbortHandler(value) || c.cfg.panicPolicy == Repanic {
			again = value
			return
		}
		if !sw.wrote {
			sw.WriteHeader(http.StatusInternalServerError)
		}
	}()
	next.ServeHTTP(sw, r)
	return nil
}
