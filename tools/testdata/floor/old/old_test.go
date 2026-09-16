package old

import "testing"

// TestValue proves the fixture module has a passing test.
func TestValue(t *testing.T) {
	if Value() != 1 {
		t.Fatal("Value changed")
	}
}
