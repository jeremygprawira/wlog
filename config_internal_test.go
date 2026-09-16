// This file proves a rule that only the package itself can see: an option resolves
// a nil redactor once, at option time.
package wlog

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

// TestCore_CORE33_NilRedactorBuiltOnce proves that WithRedactor(nil) stores
// redact.Default() once, so a logger never rebuilds a redactor per event.
func TestCore_CORE33_NilRedactorBuiltOnce(t *testing.T) {
	log := New(WithRedactor(nil))

	first := log.currentRedactor()
	if first == nil {
		t.Fatal("currentRedactor returned nil after WithRedactor(nil)")
	}
	if second := log.currentRedactor(); second != first {
		t.Error("the logger rebuilt the default redactor between two events")
	}
	if first == redact.Default() {
		t.Error("redact.Default() returns one shared instance, so this test proves nothing")
	}
}
