package wlog

import "context"

// Level is an event's severity. The zero value is not a valid Level; use LevelInfo as
// the default.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// levelRank orders levels for WithLevel's minimum-level filter.
var levelRank = map[Level]int{
	LevelDebug: 0,
	LevelInfo:  1,
	LevelWarn:  2,
	LevelError: 3,
}

// WithLevel sets the minimum level a Logger writes; events below it are dropped
// silently. Default LevelDebug (nothing filtered).
func WithLevel(min Level) Option {
	return func(l *Logger) { l.minLevel = min }
}

// SetLevel overrides the level an event would otherwise be given. Later work in a
// later task (the error extractor, C4) will set this automatically when an error is
// reported; an explicit SetLevel always wins.
func SetLevel(ctx context.Context, level Level) {
	e := eventFrom(ctx)
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		return
	}
	e.levelSet = true
	e.level = level
}
