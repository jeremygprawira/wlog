// Package new claims a Go 1.21 floor but uses the range-over-integer form that
// arrives in Go 1.22, so the floor check must fail for this module.
package new

// Sum returns the sum of 0, 1, and 2.
func Sum() int {
	total := 0
	for i := range 3 {
		total += i
	}
	return total
}
