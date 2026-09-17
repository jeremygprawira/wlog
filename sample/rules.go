package sample

import (
	"context"
	"time"
)

// tailRule force-keeps an event it matches. glob is empty for a rule that carries none, and
// is validated when the sampler is built.
type tailRule struct {
	fn   func(ctx context.Context, event map[string]any) bool
	glob string
}

// matches reports whether the rule keeps this event.
func (r tailRule) matches(ctx context.Context, event map[string]any) bool { return r.fn(ctx, event) }

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
		k.tails = append(k.tails, tailRule{fn: func(_ context.Context, event map[string]any) bool {
			status, ok := statusOf(event)
			return ok && status >= atLeast
		}})
	}
}

// KeepDuration force-keeps an event whose duration_ms is >= atLeast.
func KeepDuration(atLeast time.Duration) Option {
	atLeastMS := int(atLeast.Milliseconds())
	return func(k *keeper) {
		k.tails = append(k.tails, tailRule{fn: func(_ context.Context, event map[string]any) bool {
			ms, ok := numOf(event["duration_ms"])
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
			fn: func(_ context.Context, event map[string]any) bool {
				p, ok := pathOf(event)
				return ok && matchGlob(glob, p)
			},
		})
	}
}

// KeepFunc force-keeps an event when fn returns true.
func KeepFunc(fn func(ctx context.Context, event map[string]any) bool) Option {
	return func(k *keeper) { k.tails = append(k.tails, tailRule{fn: fn}) }
}

// statusOf reads http.status.
func statusOf(event map[string]any) (int, bool) {
	http, ok := event["http"].(map[string]any)
	if !ok {
		return 0, false
	}
	return numOf(http["status"])
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

// pathOf reads http.path.
func pathOf(event map[string]any) (string, bool) {
	http, ok := event["http"].(map[string]any)
	if !ok {
		return "", false
	}
	p, ok := http["path"].(string)
	return p, ok
}
