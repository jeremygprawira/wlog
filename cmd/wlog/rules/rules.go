// Package rules checks one HTTP handler entry point against the fixed rule set that
// wlog map scores. Rules read the handler body with type information, so they know the
// wlog, audit, fmt, and log packages by path and never import the frameworks.
package rules

import (
	"go/ast"
	"regexp"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// Rule ids. They are stable and appear in wlog.map.json.
const (
	RuleMiddleware     = "middleware.coverage"
	RuleContext        = "context.set"
	RuleErrors         = "errors.reach_wlog"
	RuleSensitiveAudit = "sensitive.audit"
	RuleNoPrint        = "logging.no_print"
	RuleNoDenylisted   = "keys.no_denylisted"
)

// Rule weights. A rule's weight counts toward a handler's score only when the rule
// applies to that handler.
const (
	WeightMiddleware     = 30
	WeightContext        = 20
	WeightErrors         = 15
	WeightSensitiveAudit = 20
	WeightNoPrint        = 10
	WeightNoDenylisted   = 5
)

// Package paths the rules recognize.
const (
	wlogPath      = "github.com/jeremygprawira/wlog"
	auditPath     = wlogPath + "/audit"
	middlewarePkg = wlogPath + "/middleware/"
)

// Rule is one id and its weight.
type Rule struct {
	ID     string
	Weight int
}

// Order returns the fixed rule order, which is also the order of checks in the map.
func Order() []Rule {
	return []Rule{
		{RuleMiddleware, WeightMiddleware},
		{RuleContext, WeightContext},
		{RuleErrors, WeightErrors},
		{RuleSensitiveAudit, WeightSensitiveAudit},
		{RuleNoPrint, WeightNoPrint},
		{RuleNoDenylisted, WeightNoDenylisted},
	}
}

// Check is one rule's result for one handler. Detail explains a failure.
type Check struct {
	ID     string `json:"id"`
	Weight int    `json:"weight"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// Config holds the user's additions to the rules.
//
// (The struct lives in config.go.)

// defaultSensitivePatterns mark routes that touch money, identity, or privilege.
var defaultSensitivePatterns = []string{
	"pay", "payment", "refund", "transfer", "auth", "login", "token",
	"password", "admin", "checkout", "withdraw", "topup", "balance",
}

// Sensitive reports whether route matches a default or configured pattern. A pattern
// is a case-insensitive regular expression when it compiles, and a case-insensitive
// substring otherwise.
func Sensitive(route string, extra []string) bool {
	patterns := make([]string, 0, len(defaultSensitivePatterns)+len(extra))
	patterns = append(patterns, defaultSensitivePatterns...)
	patterns = append(patterns, extra...)
	for _, pattern := range patterns {
		if re, err := regexp.Compile("(?i)" + pattern); err == nil {
			if re.MatchString(route) {
				return true
			}
			continue
		}
		if strings.Contains(strings.ToLower(route), strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// Evaluate runs every applicable rule for one handler.
func Evaluate(pkg *packages.Package, point entry.Point, cfg Config) []Check {
	checks := []Check{
		coverage(pkg, point),
		contextSet(pkg, point),
		errorsReachWlog(pkg, point),
	}
	if Sensitive(point.Route, cfg.SensitivePatterns) {
		checks = append(checks, sensitiveAudit(pkg, point))
	}
	checks = append(checks,
		noPrint(pkg, point),
		noDenylisted(pkg, point),
	)
	return checks
}

// pass and fail build a Check.
func pass(id string, weight int) Check {
	return Check{ID: id, Weight: weight, Pass: true}
}

func fail(id string, weight int, detail string) Check {
	return Check{ID: id, Weight: weight, Detail: detail}
}

// bodyOf returns the handler's body, whether it is a literal or a declaration.
func bodyOf(node ast.Node) *ast.BlockStmt {
	switch n := node.(type) {
	case *ast.FuncLit:
		return n.Body
	case *ast.FuncDecl:
		return n.Body
	default:
		return nil
	}
}

// bodyCalls reports whether the handler body calls a function the predicate accepts.
func bodyCalls(pkg *packages.Package, point entry.Point, predicate func(pkgPath, name string) bool) bool {
	body := bodyOf(point.Node)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		obj := entry.Callee(pkg, call)
		if obj == nil || obj.Pkg() == nil {
			return true
		}
		if predicate(obj.Pkg().Path(), obj.Name()) {
			found = true
			return false
		}
		return true
	})
	return found
}
