package rules

import (
	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// contextSet passes when the handler sets business context on its event.
func contextSet(pkg *packages.Package, point entry.Point) Check {
	if bodyCalls(pkg, point, isContextCall) {
		return pass(RuleContext, WeightContext)
	}
	return fail(RuleContext, WeightContext, "handler sets no business context")
}

// isContextCall reports whether a call adds context to the event: a wlog setter, an
// error report, or an audit fact.
func isContextCall(pkgPath, name string) bool {
	if pkgPath == auditPath {
		return name == "Do"
	}
	if pkgPath != wlogPath {
		return false
	}
	switch name {
	case "Set", "SetGroup", "Append", "Error", "Info", "Warn", "Debug":
		return true
	default:
		return false
	}
}
