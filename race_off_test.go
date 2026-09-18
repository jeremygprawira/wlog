//go:build !race

package wlog_test

// raceEnabled reports that this test binary was built without the race detector, so a
// wall-clock budget measures the code rather than the detector.
const raceEnabled = false
