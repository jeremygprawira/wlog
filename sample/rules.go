package sample

import (
	"context"
	"time"

	"github.com/jeremygprawira/wlog"
)

// tailRule force-keeps an event it matches. glob is empty for a rule that carries none, and
// is validated when the sampler is built.
type tailRule struct {
	fn   func(ctx context.Context, event wlog.Event) bool
	glob string
}

// matches reports whether the rule keeps this event.
func (r tailRule) matches(ctx context.Context, event wlog.Event) bool { return r.fn(ctx, event) }

// valid reports whether the rule can ever match.
func (r tailRule) valid() error {
	if r.glob == "" {
		return nil
	}
	return validateGlob(r.glob)
}

// KeepStatus force-keeps an event whose http.status is >= atLeast.
func KeepStatus(atLeast int) Option {
	return func(k *keeper) {
		k.tails = append(k.tails, tailRule{fn: func(_ context.Context, event wlog.Event) bool {
			status, ok := intAt(event, "http.status")
			return ok && status >= atLeast
		}})
	}
}

// KeepDuration force-keeps an event whose duration_ms is >= atLeast.
func KeepDuration(atLeast time.Duration) Option {
	atLeastMS := int(atLeast.Milliseconds())
	return func(k *keeper) {
		k.tails = append(k.tails, tailRule{fn: func(_ context.Context, event wlog.Event) bool {
			ms, ok := intAt(event, "duration_ms")
			return ok && ms >= atLeastMS
		}})
	}
}

// KeepPath force-keeps an event whose http.path matches glob. A ** segment matches any run of
// segments, including none, so "/api/**" covers "/api" and "/api/payments/123"; every other
// segment keeps path.Match semantics, where * stays inside one segment.
func KeepPath(glob string) Option {
	return func(k *keeper) {
		k.tails = append(k.tails, tailRule{
			glob: glob,
			fn: func(_ context.Context, event wlog.Event) bool {
				value, ok := event.Get("http.path")
				if !ok {
					return false
				}
				p, ok := value.(string)
				return ok && matchGlob(glob, p)
			},
		})
	}
}

// KeepFunc force-keeps an event when fn returns true.
func KeepFunc(fn func(ctx context.Context, event wlog.Event) bool) Option {
	return func(k *keeper) { k.tails = append(k.tails, tailRule{fn: fn}) }
}

// intAt reads a number at a dotted path, such as "http.status" or "duration_ms", and
// reports whether the field is there with a number in it.
func intAt(event wlog.Event, path string) (int, bool) {
	value, ok := event.Get(path)
	if !ok {
		return 0, false
	}
	return numOf(value)
}

// numOf coerces any of the numeric shapes an event field might carry (a literal Go
// int/int64 before JSON, or a float64 after a JSON round-trip) into an int.
func numOf(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
