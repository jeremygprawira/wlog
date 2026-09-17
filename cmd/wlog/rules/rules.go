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
	RuleErrorGuidance  = "error-guidance"
	RuleSwallowedError = "swallowed-error"
	RuleUseCatalog     = "use-catalog"
	RuleAuditCoverage  = "audit-coverage"
	RuleKeysStrict     = "keys.strict"
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
	WeightErrorGuidance  = 15
	WeightSwallowedError = 15
	// A near-miss key is a correctness lint rather than a coverage gap, so the rule reports
	// without moving a score: the same choice use-catalog and audit-coverage make.
	WeightKeysStrict = 0
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
		{RuleErrorGuidance, WeightErrorGuidance},
		{RuleSwallowedError, WeightSwallowedError},
		{RuleSensitiveAudit, WeightSensitiveAudit},
		{RuleNoPrint, WeightNoPrint},
		{RuleNoDenylisted, WeightNoDenylisted},
		{RuleKeysStrict, WeightKeysStrict},
		{RuleUseCatalog, 0},
		{RuleAuditCoverage, 0},
	}
}

// Check is one rule's result for one handler. Detail explains a failure. A suggestion
// carries weight 0 and never changes the score.
type Check struct {
	ID     string `json:"id"`
	Weight int    `json:"weight"`
	Pass   bool   `json:"pass"`
	// Applicable false means the rule had nothing to check on this handler, and the report
	// shows it as n/a. An n/a rule adds no points and no weight, so it never hands a handler
	// free points for a rule it never ran (CLI-7).
	Applicable bool   `json:"applicable"`
	Detail     string `json:"detail"`
	Suggestion bool   `json:"suggestion,omitempty"`
}

// Config holds the user's additions to the rules.
//
// (The struct lives in config.go.)

// defaultSensitivePatterns mark routes that touch money, identity, or privilege.
var defaultSensitivePatterns = []string{
	"pay", "payment", "refund", "transfer", "auth", "login", "token",
	"password", "admin", "checkout", "withdraw", "topup", "balance",
}

// Sensitive reports whether route matches a default or configured pattern.
//
// A pattern that compiles as a regular expression matches the route as written. Any other
// pattern matches a WHOLE path segment or a whole word inside one, so "auth" covers /auth/login
// and /user/auth-token, and never /authors: a substring match marked a route that only resembles
// a sensitive one, which made the report cry wolf (CLI-14).
func Sensitive(route string, extra []string) bool {
	patterns := make([]string, 0, len(defaultSensitivePatterns)+len(extra))
	patterns = append(patterns, defaultSensitivePatterns...)
	patterns = append(patterns, extra...)
	segments := routeSegments(route)
	for _, pattern := range patterns {
		if looksLikeRegex(pattern) {
			if re, err := regexp.Compile("(?i)" + pattern); err == nil && re.MatchString(route) {
				return true
			}
			continue
		}
		lower := strings.ToLower(pattern)
		for _, segment := range segments {
			if segment == lower || containsWord(segment, lower) {
				return true
			}
		}
	}
	return false
}

// looksLikeRegex reports whether a pattern is a regular expression rather than a word. A plain
// word is also a valid pattern, and matching it as a regex is how "auth" came to match
// "/authors".
func looksLikeRegex(pattern string) bool {
	return strings.ContainsAny(pattern, `^$*+?()[]{}|\`)
}

// routeSegments splits a route into its lower-case path segments, dropping the empty ones.
func routeSegments(route string) []string {
	parts := strings.Split(strings.ToLower(route), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// containsWord reports whether one path segment holds word as a whole word, split on the
// separators a route name uses.
func containsWord(segment, word string) bool {
	for _, part := range strings.FieldsFunc(segment, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '{' || r == '}' || r == ':'
	}) {
		if part == word || isFormOf(part, word) {
			return true
		}
	}
	return false
}

// isFormOf reports whether a segment word is a plural or verb form of the pattern, so the pattern
// "payment" covers /payments and the pattern "transfer" covers /transferred. A longer unrelated
// word stays unmatched, which is what keeps "auth" away from /authors.
func isFormOf(word, pattern string) bool {
	for _, suffix := range []string{"s", "es", "ed", "ing", "d"} {
		if word == pattern+suffix {
			return true
		}
	}
	return false
}

// Class returns one entry point's class: "sensitive" for a money, auth, or admin route,
// "write" for a state-changing method or route, and "read" otherwise.
func Class(point entry.Point) string {
	if point.Sensitive || Sensitive(point.Route, nil) {
		return "sensitive"
	}
	if isWriteRoute(point) {
		return "write"
	}
	return "read"
}

// Evaluate runs every applicable rule for one handler.
// Evaluate runs every rule that applies to one handler. program is the whole loaded package
// set, which a rule that must look beyond one package, such as middleware coverage, searches.
func Evaluate(program []*packages.Package, pkg *packages.Package, point entry.Point, cfg Config) []Check {
	checks := []Check{
		coverage(program, pkg, point),
		contextSet(pkg, point),
		errorsReachWlog(pkg, point),
		errorGuidance(pkg, point),
		swallowedError(pkg, point),
	}
	if Sensitive(point.Route, cfg.SensitivePatterns) {
		checks = append(checks, sensitiveAudit(pkg, point))
	}
	checks = append(checks,
		noPrint(pkg, point),
		noDenylisted(pkg, point),
		keysStrict(pkg, point),
		useCatalog(pkg, point),
		auditCoverage(pkg, point),
	)
	return checks
}

// pass and fail build a Check.
func pass(id string, weight int) Check {
	return Check{ID: id, Weight: weight, Pass: true, Applicable: true}
}

// notApplicable builds a Check for a rule that had nothing to check on this handler.
func notApplicable(id string, weight int) Check {
	return Check{ID: id, Weight: weight, Applicable: false}
}

func fail(id string, weight int, detail string) Check {
	return Check{ID: id, Weight: weight, Applicable: true, Detail: detail}
}

// suggest builds a failed suggestion, which never changes the score.
func suggest(id, detail string) Check {
	return Check{ID: id, Detail: detail, Suggestion: true}
}

// passSuggestion builds a passing suggestion.
func passSuggestion(id string) Check {
	return Check{ID: id, Pass: true, Suggestion: true}
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
