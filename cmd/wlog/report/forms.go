package report

import (
	"fmt"
	"strings"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// Matrix renders one row per entry point against one column per rule, so a reader sees
// where the gaps cluster. The bytes are deterministic: handlers are already sorted, and
// the rule order is fixed.
func Matrix(m Map) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "score %d  grade %s  handlers %d\n", m.Score, m.Grade, len(m.Handlers))

	columns := rules.Order()
	fmt.Fprintf(&builder, "%-40s %-9s", "entry", "class")
	for _, rule := range columns {
		fmt.Fprintf(&builder, " %s", shortRule(rule.ID))
	}
	builder.WriteByte('\n')

	for _, handler := range m.Handlers {
		entryName := handler.Function
		if handler.Route != "" {
			entryName = handler.Route
		}
		fmt.Fprintf(&builder, "%-40s %-9s", truncate(entryName, 40), handler.Class)
		byID := map[string]rules.Check{}
		for _, check := range handler.Checks {
			byID[check.ID] = check
		}
		for _, rule := range columns {
			check, present := byID[rule.ID]
			switch {
			case !present:
				fmt.Fprintf(&builder, " %-6s", "-")
			case !check.Applicable:
				// The rule had nothing to check on this handler, so the cell says so instead of
				// claiming a pass or a failure.
				fmt.Fprintf(&builder, " %-6s", "n/a")
			case check.Pass:
				fmt.Fprintf(&builder, " %-6s", "ok")
			case check.Suggestion:
				fmt.Fprintf(&builder, " %-6s", "??")
			default:
				fmt.Fprintf(&builder, " %-6s", "XX")
			}
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

// Entry renders the full detail for one entry point, matched by function name or route.
// It reports false when no entry point matches.
func Entry(m Map, name string) (string, bool) {
	for _, handler := range m.Handlers {
		if handler.Function != name && handler.Route != name {
			continue
		}
		var builder strings.Builder
		fmt.Fprintf(&builder, "%s  %s  %s:%d\n", handler.Function, handler.Class, handler.File, handler.Line)
		if handler.Route != "" {
			fmt.Fprintf(&builder, "route %s %s\n", handler.Method, handler.Route)
		}
		for _, check := range handler.Checks {
			mark := "PASS"
			switch {
			case !check.Applicable:
				mark = "n/a"
			case !check.Pass && check.Suggestion:
				mark = "SUGGEST"
			case !check.Pass:
				mark = "FAIL"
			}
			fmt.Fprintf(&builder, "%-22s %s  %s\n", check.ID, mark, check.Detail)
		}
		return builder.String(), true
	}
	return "", false
}

// shortRule trims a rule id to fit a matrix column.
func shortRule(id string) string {
	switch id {
	case "middleware.coverage":
		return "mw"
	case "context.set":
		return "ctx"
	case "errors.reach_wlog":
		return "err"
	case "error-guidance":
		return "guide"
	case "swallowed-error":
		return "swallow"
	case "sensitive.audit":
		return "audit"
	case "logging.no_print":
		return "print"
	case "keys.no_denylisted":
		return "keys"
	case "use-catalog":
		return "catalog"
	case "audit-coverage":
		return "audit+"
	default:
		return truncate(id, 6)
	}
}

// truncate shortens s to n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
