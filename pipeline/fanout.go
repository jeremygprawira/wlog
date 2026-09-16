package pipeline

import (
	"context"
	"fmt"

	"github.com/jeremygprawira/wlog"
)

// FanOut returns a wlog.Drain whose Send delivers to every drain concurrently, so
// one slow or hanging drain never delays delivery to the others.
//
// Each delivery runs under recover. A drain that panics inside its own goroutine
// would otherwise take the process with it, and the logger's own guard cannot reach
// a goroutine that FanOut started.
func FanOut(drains ...wlog.Drain) wlog.Drain {
	return &fanOut{drains: drains}
}

// fanOut delivers one event to every drain at the same time.
type fanOut struct {
	drains []wlog.Drain
	report func(err error, source string)
}

// WithReporter sets the function that hears about a drain which panicked. It is
// optional, and a FanOut without one stays silent rather than panicking.
func WithReporter(fn func(err error, source string)) func(*fanOut) {
	return func(f *fanOut) { f.report = fn }
}

// Send delivers the event to every drain, each in its own goroutine.
func (f *fanOut) Send(ctx context.Context, event map[string]any) {
	for _, d := range f.drains {
		go func(d wlog.Drain) {
			defer func() {
				if r := recover(); r != nil && f.report != nil {
					f.report(fmt.Errorf("panic: %v", r), "drain")
				}
			}()
			d.Send(ctx, event)
		}(d)
	}
}
