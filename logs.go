package wlog

import "context"

// maxLogLines bounds an event's logs[] array (SPEC-core), so a chatty library logging
// inside a request cannot grow one event without limit (gate G4).
const maxLogLines = 50

// LogLine is one log record folded into an event's logs[] array by a log-*-in adapter
// (today, log/slog's Handler). Attrs holds the record's own fields, already resolved.
type LogLine struct {
	Level string         `json:"level"`
	Msg   string         `json:"msg"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// AppendLog folds one LogLine into the current event's logs[] array. It is a no-op,
// never a panic, when ctx carries no event or the event already sealed. A line beyond
// maxLogLines is dropped and counted in wlog.dropped_logs.
func AppendLog(ctx context.Context, line LogLine) {
	e := eventFrom(ctx)
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		e.recordLateWrite()
		return
	}

	arr, ok := e.fields["logs"].([]any)
	if !ok {
		if !e.reserveTopLevelSlot("logs") {
			return
		}
	}
	if len(arr) >= maxLogLines {
		e.droppedLogs++
		return
	}
	e.fields["logs"] = append(arr, normalize(line))
}
