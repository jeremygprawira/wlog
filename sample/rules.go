package sample

import (
	"context"
	"path"
	"time"
)

// KeepStatus force-keeps an event whose http.status is >= atLeast.
func KeepStatus(atLeast int) Option {
	return func(k *keeper) {
		k.tails = append(k.tails, func(_ context.Context, event map[string]any) bool {
			status, ok := statusOf(event)
			return ok && status >= atLeast
		})
	}
}

// KeepDuration force-keeps an event whose duration_ms is >= atLeast.
func KeepDuration(atLeast time.Duration) Option {
	atLeastMS := int(atLeast.Milliseconds())
	return func(k *keeper) {
		k.tails = append(k.tails, func(_ context.Context, event map[string]any) bool {
			ms, ok := numOf(event["duration_ms"])
			return ok && ms >= atLeastMS
		})
	}
}

// KeepPath force-keeps an event whose http.path matches glob (path.Match syntax,
// e.g. "/api/payments/**").
func KeepPath(glob string) Option {
	return func(k *keeper) {
		k.tails = append(k.tails, func(_ context.Context, event map[string]any) bool {
			p, ok := pathOf(event)
			if !ok {
				return false
			}
			matched, err := path.Match(glob, p)
			return err == nil && matched
		})
	}
}

// KeepFunc force-keeps an event when fn returns true.
func KeepFunc(fn func(ctx context.Context, event map[string]any) bool) Option {
	return func(k *keeper) { k.tails = append(k.tails, fn) }
}

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

func pathOf(event map[string]any) (string, bool) {
	http, ok := event["http"].(map[string]any)
	if !ok {
		return "", false
	}
	p, ok := http["path"].(string)
	return p, ok
}
