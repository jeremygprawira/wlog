package rules_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// TestMap_BET22_StrictKeys proves keys.strict flags a literal key that is a near miss of a
// declared typed key, at the handler that writes it, and leaves the exact spelling and an
// unrelated key alone.
func TestMap_BET22_StrictKeys(t *testing.T) {
	pkgs, err := entry.Load("./../testdata/strictkeys_app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byFunction := map[string][]rules.Check{}
	for _, point := range entry.Find(pkgs) {
		for _, p := range pkgs {
			if p.PkgPath != point.Package {
				continue
			}
			for _, check := range rules.Evaluate(pkgs, p, point, rules.Config{}) {
				if check.ID == rules.RuleKeysStrict {
					byFunction[point.Function] = append(byFunction[point.Function], check)
				}
			}
		}
	}

	nearMiss := byFunction["nearMiss"]
	if len(nearMiss) == 0 {
		t.Fatalf("keys.strict did not run for nearMiss: %v", byFunction)
	}
	if nearMiss[0].Pass {
		t.Errorf("keys.strict passed for a near-miss key: %+v", nearMiss[0])
	}
	if nearMiss[0].Detail == "" {
		t.Error("keys.strict failed without naming the key it saw")
	}

	for _, function := range []string{"exact"} {
		checks := byFunction[function]
		if len(checks) == 0 {
			t.Fatalf("keys.strict did not run for %s", function)
		}
		if !checks[0].Pass {
			t.Errorf("keys.strict failed for %s: %+v", function, checks[0])
		}
	}
}
