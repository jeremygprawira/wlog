//go:build race

package wlog_test

// raceEnabled reports that this test binary was built with the race detector, which
// inflates the emit path about ten times. A test with a wall-clock budget reads it.
const raceEnabled = true
