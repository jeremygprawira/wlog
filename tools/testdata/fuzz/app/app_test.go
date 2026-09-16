package app

import "testing"

// FuzzAdd proves the finder reports a fuzz target.
func FuzzAdd(f *testing.F) {
	f.Add(1, 2)
	f.Fuzz(func(t *testing.T, a, b int) { _ = a + b })
}

// FuzzSub proves the finder reports a second target in one package.
func FuzzSub(f *testing.F) {
	f.Add(2, 1)
	f.Fuzz(func(t *testing.T, a, b int) { _ = a - b })
}

// TestPlain proves a plain test never counts as a target.
func TestPlain(t *testing.T) {}
