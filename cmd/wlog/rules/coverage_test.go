package rules_test

import (
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// TestMap_CLI9_MiddlewareAcrossPackages proves the middleware rule credits a handler whose
// router is wrapped in another package, which is the common layout: main wraps what the server
// package builds.
func TestMap_CLI9_MiddlewareAcrossPackages(t *testing.T) {
	pkgs, err := entry.Load("./../testdata/middleware_app/...")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	program := entry.Program(pkgs)
	if len(program) < 2 {
		t.Fatalf("the fixture loaded %d packages, want the app and its server package", len(program))
	}

	scored := 0
	for _, point := range entry.Find(pkgs) {
		pkg := packageOf(program, point.Package)
		if pkg == nil {
			continue
		}
		scored++
		for _, check := range rules.Evaluate(program, pkg, point, rules.Config{}) {
			if check.ID == rules.RuleMiddleware && !check.Pass {
				t.Errorf("middleware coverage failed for %s.%s: %s", point.Package, point.Function, check.Detail)
			}
		}
	}
	if scored == 0 {
		t.Fatal("the fixture registered no handler")
	}
}

// packageOf returns the loaded package with this path, or nil.
func packageOf(program []*packages.Package, path string) *packages.Package {
	for _, pkg := range program {
		if pkg.PkgPath == path {
			return pkg
		}
	}
	return nil
}
