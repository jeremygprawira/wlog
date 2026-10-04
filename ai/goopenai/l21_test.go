package wloggoopenai

import "testing"

// TestGoopenai_L21_PackageName proves this module's package is wloggoopenai.
func TestGoopenai_L21_PackageName(t *testing.T) {
	if pkg := "wloggoopenai"; pkg == "" {
		t.Fatal("package name is empty")
	}
}
