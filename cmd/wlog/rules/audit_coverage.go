package rules

import (
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// auditCoverage suggests an audit record for a handler on a write route that records
// none. It is a suggestion, so it never touches the score, and it only fires on a route
// that changes state.
func auditCoverage(pkg *packages.Package, point entry.Point) Check {
	if !isWriteRoute(point) {
		return passSuggestion(RuleAuditCoverage)
	}
	if bodyCalls(pkg, point, isAuditCall) {
		return passSuggestion(RuleAuditCoverage)
	}
	return suggest(RuleAuditCoverage, "write route records no audit")
}

// isWriteRoute reports whether the method or the route says the handler changes state.
func isWriteRoute(point entry.Point) bool {
	switch strings.ToUpper(point.Method) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	lower := strings.ToLower(point.Route)
	for _, word := range []string{"create", "update", "delete", "insert", "upsert", "refund"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// isAuditCall reports whether the call records an audit fact.
func isAuditCall(pkgPath, name string) bool {
	if pkgPath != auditPath {
		return false
	}
	switch name {
	case "Do", "Deny", "Only", "Wrap":
		return true
	default:
		return false
	}
}
