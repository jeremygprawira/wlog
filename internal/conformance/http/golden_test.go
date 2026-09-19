// This file checks the golden files of the suite: every scenario that starts an event has
// a hand-written golden, and every golden parses.
package httpconformance

import "testing"

// TestConformance_HTTP22_EveryScenarioHasGolden proves that the suite holds the 19
// scenarios of SPEC-conformance.md, and that every scenario which starts an event has a
// golden file. The suite then never compares against a missing document.
func TestConformance_HTTP22_EveryScenarioHasGolden(t *testing.T) {
	if len(scenarios) != 19 {
		t.Errorf("scenarios = %d, want the 19 of SPEC-conformance.md", len(scenarios))
	}
	for _, scenario := range scenarios {
		_, err := golden(scenario.Name)
		if scenario.NoEvents {
			if err == nil {
				t.Errorf("%s starts no event, so it needs no golden", scenario.Name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", scenario.Name, err)
		}
	}
}
