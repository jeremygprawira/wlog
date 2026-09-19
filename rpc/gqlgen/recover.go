// This file holds the recover wrapper: a resolver panic reaches the event as an error with
// a stack, and the app's own recover function still answers the client.
package wloggqlgen

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/99designs/gqlgen/graphql"

	"github.com/jeremygprawira/wlog"
)

// Recover wraps next, the app's own recover function, with one that records the panic on
// the event first. It never replaces next: when next is nil, graphql.DefaultRecover
// answers the client. Set the result with handler.SetRecoverFunc.
func Recover(next graphql.RecoverFunc) graphql.RecoverFunc {
	return func(ctx context.Context, value any) error {
		wlog.Error(ctx, &panicError{value: value, stack: string(debug.Stack())})
		if next != nil {
			return next(ctx, value)
		}
		return graphql.DefaultRecover(ctx, value)
	}
}

// panicError carries a recovered panic value and the stack of the moment it was recovered.
// The default extractor of core reads the stack through the Stack method.
type panicError struct {
	value any
	stack string
}

// Error returns the panic value as a message.
func (e *panicError) Error() string { return fmt.Sprintf("panic: %v", e.value) }

// Stack returns the stack of the panic.
func (e *panicError) Stack() string { return e.stack }
