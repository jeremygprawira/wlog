package wlog

import (
	"context"
	"fmt"
)

// Level is an event's severity. The zero value is not a valid Level; use LevelInfo as
// the default.
type Level string

const (
	// LevelDebug is the level of a step worth reading while an event is built.
	LevelDebug Level = "debug"
	// LevelInfo is the level of a unit of work that succeeded.
	LevelInfo Level = "info"
	// LevelWarn is the level of a problem that the caller recovered from.
	LevelWarn Level = "warn"
	// LevelError is the level of a unit of work that failed.
	LevelError Level = "error"
)

// levelRank orders levels for WithLevel's minimum-level filter.
var levelRank = map[Level]int{
	LevelDebug: 0,
	LevelInfo:  1,
	LevelWarn:  2,
	LevelError: 3,
}

// CurrentLevel returns the level of the current event, and reports whether SetLevel named
// it. A finisher reads the report, so a level an earlier stage chose wins over the level the
// finisher sets. A context with no event reports no level.
func CurrentLevel(ctx context.Context) (Level, bool) {
	e := eventFrom(ctx)
	if e == nil {
		return "", false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.level, e.levelSet
}

// WithLevel sets the minimum level a Logger writes. An event below it is dropped, and
// debug mode names the reason. Default LevelDebug (nothing filtered).
func WithLevel(min Level) Option {
	return func(l *Logger) {
		if !validLevel(min) {
			// Report and keep the current level, so a typo never silences a
			// service or turns it into noise.
			l.reportProblem(codeInvalidConfig, "WithLevel", fmt.Errorf("unknown level %q", string(min)))
			return
		}
		l.minLevel = min
	}
}

// validLevel reports whether a level is one of the four.
func validLevel(level Level) bool {
	_, ok := levelRank[level]
	return ok
}

// SetLevel overrides the level an event would otherwise be given. Later work in a
// later task (the error extractor, C4) will set this automatically when an error is
// reported; an explicit SetLevel always wins.
func SetLevel(ctx context.Context, level Level) {
	e := eventFrom(ctx)
	if e == nil {
		noEvent(ctx, "level")
		return
	}
	if !validLevel(level) {
		// Report and keep the event's own level, so a typo never labels an event
		// with a severity no filter understands.
		if l := loggerFrom(e.ctx); l != nil {
			l.reportProblem(codeInvalidConfig, "SetLevel", fmt.Errorf("unknown level %q", string(level)))
		}
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		e.recordLateWrite()
		return
	}
	e.levelSet = true
	e.level = level
}
