package rules

import (
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// RuleIgnoreReason is the rule a directive with no reason breaks: a suppression is a claim that
// the rule does not apply here, and the claim needs a reason a reviewer can weigh.
const RuleIgnoreReason = "ignore.reason"

// directivePrefix starts an ignore comment.
const directivePrefix = "wlog:ignore"

// directive is one parsed ignore comment.
type directive struct {
	rule   string
	reason string
	// missingReason is true for a directive that names no reason, which is a finding.
	missingReason bool
}

// parseDirective reads one comment as a directive. It reports false when the comment is not one.
func parseDirective(text string) (directive, bool) {
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "//"))
	if !strings.HasPrefix(body, directivePrefix) {
		return directive{}, false
	}
	body = strings.TrimSpace(strings.TrimPrefix(body, directivePrefix))
	rule, reason, hasReason := strings.Cut(body, "--")
	rule = strings.TrimSpace(rule)
	reason = strings.TrimSpace(reason)
	if rule == "" {
		return directive{}, false
	}
	return directive{rule: rule, reason: reason, missingReason: !hasReason || reason == ""}, true
}

// directivesFor returns every directive that applies to one handler: a comment on the handler's
// own line, or on the line before it.
func directivesFor(pkg *packages.Package, point entry.Point) []directive {
	if point.Node == nil {
		return nil
	}
	position := pkg.Fset.Position(point.Node.Pos())
	var out []directive
	for _, file := range pkg.Syntax {
		for _, group := range file.Comments {
			line := pkg.Fset.Position(group.Pos()).Line
			if line != position.Line && line != position.Line-1 {
				continue
			}
			for _, comment := range group.List {
				if directive, ok := parseDirective(comment.Text); ok {
					out = append(out, directive)
				}
			}
		}
	}
	return out
}

// applyIgnores marks the checks a directive suppresses, and adds one finding per directive that
// names no reason.
func applyIgnores(pkg *packages.Package, point entry.Point, checks []Check) []Check {
	directives := directivesFor(pkg, point)
	if len(directives) == 0 {
		return checks
	}
	for _, directive := range directives {
		if directive.missingReason {
			check := fail(RuleIgnoreReason, 0, "ignore directive for "+directive.rule+" has no reason after --")
			check.Node = point.Node
			checks = append(checks, check)
			continue
		}
		for i := range checks {
			if checks[i].ID == directive.rule && checks[i].Applicable && !checks[i].Pass {
				checks[i].Suppressed = true
				checks[i].Reason = directive.reason
			}
		}
	}
	return checks
}

// SuppressedCount returns how many checks a directive suppressed, which the report prints so a
// reader knows the score was shaped by a claim rather than by the code.
func SuppressedCount(checks []Check) int {
	count := 0
	for _, check := range checks {
		if check.Suppressed {
			count++
		}
	}
	return count
}
