// This file checks the golden files of the suite: every scenario that starts an event has
// a hand-written golden, and every golden parses.
package httpconformance

import "testing"

// TestConformance_HTTP22_EveryScenarioHasGolden proves that every scenario which starts an
// event has a golden file, so the suite never compares against a missing document.
func TestConformance_HTTP22_EveryScenarioHasGolden(t *testing.T) {
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
