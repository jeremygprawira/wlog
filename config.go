package wlog

import (
	"fmt"
	"os"
	"strings"
)

// envWarning is a problem found while reading env vars in New, reported via OnError
// only after every Option has run (so a later OnError option is already in place to
// receive it).
type envWarning struct {
	err    error
	source string
}

// applyEnvDefaults reads WLOG_SERVICE, WLOG_VERSION, WLOG_ENV, WLOG_LEVEL, and
// WLOG_FORMAT as the Logger's starting configuration. Called before any Option, so an
// explicit option (WithService, WithLevel, WithFormat, ...) always overrides the
// matching env var. An invalid WLOG_LEVEL or WLOG_FORMAT value is left at its default
// and returned as a warning; it never panics and never blocks New from returning.
func (l *Logger) applyEnvDefaults() []envWarning {
	var warnings []envWarning

	l.service.name = os.Getenv("WLOG_SERVICE")
	l.service.version = os.Getenv("WLOG_VERSION")
	l.service.env = os.Getenv("WLOG_ENV")

	if v := os.Getenv("WLOG_LEVEL"); v != "" {
		if lvl, ok := parseLevel(v); ok {
			l.minLevel = lvl
		} else {
			warnings = append(warnings, envWarning{
				err:    fmt.Errorf("WLOG_LEVEL: invalid value %q, using default", v),
				source: "WLOG_LEVEL",
			})
		}
	}

	if v := os.Getenv("WLOG_FORMAT"); v != "" {
		if f, ok := parseFormat(v); ok {
			l.format = f
		} else {
			warnings = append(warnings, envWarning{
				err:    fmt.Errorf("WLOG_FORMAT: invalid value %q, using default", v),
				source: "WLOG_FORMAT",
			})
		}
	}

	return warnings
}

func parseLevel(s string) (Level, bool) {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug, true
	case "info":
		return LevelInfo, true
	case "warn", "warning":
		return LevelWarn, true
	case "error":
		return LevelError, true
	default:
		return "", false
	}
}

func parseFormat(s string) (Format, bool) {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON, true
	case "pretty":
		return FormatPretty, true
	case "auto":
		return FormatAuto, true
	default:
		return 0, false
	}
}
