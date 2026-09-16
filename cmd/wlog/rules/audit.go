package rules

import (
	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// sensitiveAudit checks a sensitive route. The weight is doubled, so a forgotten audit
// fact on a payment or auth route costs more than any other single miss.
func sensitiveAudit(pkg *packages.Package, point entry.Point) Check {
	weight := 2 * WeightSensitiveAudit
	if bodyCalls(pkg, point, func(pkgPath, name string) bool {
		return pkgPath == auditPath && name == "Do"
	}) {
		return pass(RuleSensitiveAudit, weight)
	}
	return fail(RuleSensitiveAudit, weight, "sensitive route does not call audit.Do")
}
