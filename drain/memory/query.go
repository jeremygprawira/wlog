package memory

import (
	"reflect"
	"time"
)

// Filter selects events from a Memory. A zero field means "any".
type Filter struct {
	Level    string                    // exact match on the level field, empty means any
	Since    time.Time                 // inclusive, zero means any
	Until    time.Time                 // inclusive, zero means any
	Contains map[string]any            // every pair must match by exact value
	Match    func(map[string]any) bool // runs last, after the cheap checks
	Limit    int                       // 0 means every match
}

// Query returns the matching events, oldest first.
//
// Limit keeps the NEWEST matches rather than the oldest: a caller that asks for the last ten
// errors wants the ten most recent ones, not the first ten a process ever logged.
func (m *Memory) Query(f Filter) []map[string]any {
	matches := make([]map[string]any, 0)
	for _, event := range m.Snapshot() {
		if matchesFilter(event, f) {
			matches = append(matches, event)
		}
	}
	if f.Limit > 0 && len(matches) > f.Limit {
		matches = matches[len(matches)-f.Limit:]
	}
	return matches
}

// Matches reports whether one event passes the filter. Read and Tail in drain/file use
// it, so one filter type covers every reader.
func Matches(event map[string]any, f Filter) bool { return matchesFilter(event, f) }

// matchesFilter reports whether one event passes every filter field, cheapest first.
func matchesFilter(event map[string]any, f Filter) bool {
	if f.Level != "" {
		if level, _ := event["level"].(string); level != f.Level {
			return false
		}
	}
	if !f.Since.IsZero() || !f.Until.IsZero() {
		timestamp, ok := eventTime(event)
		if !ok {
			return false
		}
		if !f.Since.IsZero() && timestamp.Before(f.Since) {
			return false
		}
		if !f.Until.IsZero() && timestamp.After(f.Until) {
			return false
		}
	}
	for key, want := range f.Contains {
		got, ok := event[key]
		if !ok || !reflect.DeepEqual(got, want) {
			return false
		}
	}
	return f.Match == nil || f.Match(event)
}

// eventTime reads the event's timestamp as a time.
func eventTime(event map[string]any) (time.Time, bool) {
	text, ok := event["timestamp"].(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
