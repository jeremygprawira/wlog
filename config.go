package wlog

import (
	"fmt"
	"os"
	"strings"
)

// envWarning is a problem found while reading env vars in New, reported via OnProblem
// only after every Option has run (so a later OnProblem option is already in place to
// receive it).
type envWarning struct {
	err    error
	source string
}

// applyEnvDefaults reads the logger vars and the service identity as the Logger's
// starting configuration. Called before any Option, so an explicit option (WithService,
// WithLevel, WithFormat, WithDebug, ...) always overrides the matching env var. An
// invalid WLOG_LEVEL or WLOG_FORMAT value is left at its default and returned as a
// warning; it never panics and never blocks New from returning.
func (l *Logger) applyEnvDefaults() []envWarning {
	var warnings []envWarning

	l.service.name = firstEnv("WLOG_SERVICE", "OTEL_SERVICE_NAME", "SERVICE_NAME")
	l.service.version = firstEnv("WLOG_VERSION", "APP_VERSION", "SERVICE_VERSION")
	l.service.env = firstEnv("WLOG_ENV", "APP_ENV", "ENVIRONMENT",
		otelResourceValue("deployment.environment.name"))

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

	// WLOG_DEBUG=1 turns on debug mode: every dropped event reports its reason.
	if v := os.Getenv("WLOG_DEBUG"); v == "1" || strings.EqualFold(v, "true") {
		l.debug = true
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

// firstEnv returns the first name that holds a non-empty value.
func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

// otelResourceValue reads one key from OTEL_RESOURCE_ATTRIBUTES, which holds pairs of a
// key and a value separated by commas.
func otelResourceValue(key string) string {
	for _, pair := range strings.Split(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"), ",") {
		name, value, found := strings.Cut(pair, "=")
		if found && strings.TrimSpace(name) == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
