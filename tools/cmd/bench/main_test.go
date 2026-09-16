// Package main tests the bench command with a stubbed comparison.
//
// The stub replaces the benchmark run and benchstat, so the tests stay offline
// and prove the threshold, the report, and the sign handling.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// baselineText is the fixture baseline. The file itself holds two runs of two
// benchmarks, which benchstat reads.
const baselineText = `goos: darwin
goarch: arm64
pkg: example.com/app
cpu: Apple M3
BenchmarkFast-8         	  100000	      1234 ns/op
BenchmarkFast-8         	  100000	      1250 ns/op
BenchmarkSlow-8         	   50000	      9000 ns/op
BenchmarkSlow-8         	   50000	      9100 ns/op
`

// fixture writes the baseline and returns the paths of the directory and the
// baseline file.
func fixture(t *testing.T) (dir, baseline string) {
	t.Helper()
	dir = t.TempDir()
	baseline = filepath.Join(dir, "baseline.txt")
	if err := os.WriteFile(baseline, []byte(baselineText), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, baseline
}

// report is a benchstat report with one slowdown and one small change.
const report = `goos: darwin
goarch: arm64
pkg: example.com/app
                 │   old.txt    │             new.txt              │
                 │    sec/op    │   sec/op     vs base             │
BenchmarkFast-8     1.240µ ± 1%   1.260µ ± 1%   +1.61% (p=0.002 n=10)
BenchmarkSlow-8     9.050µ ± 2%  11.312µ ± 2%  +25.00% (p=0.000 n=10)
geomean             3.349µ        3.774µ        +12.69%
`

// TestBench_FailsOnSlowdown proves that a benchmark which is slower than the
// threshold fails the run and the report names it.
func TestBench_FailsOnSlowdown(t *testing.T) {
	dir, baseline := fixture(t)
	bench := func(dir string) ([]byte, error) { return []byte("current"), nil }
	compare := func(base, current []byte) ([]byte, error) { return []byte(report), nil }

	var out bytes.Buffer
	if err := check(dir, baseline, 20, bench, compare, &out); err == nil {
		t.Fatal("check returned nil, want an error for the slowdown")
	}
	got := out.String()
	if !strings.Contains(got, "BENCH1") || !strings.Contains(got, "BenchmarkSlow-8") {
		t.Errorf("output misses the code or the benchmark:\n%s", got)
	}
	if strings.Contains(got, "BenchmarkFast-8") {
		t.Errorf("output reports a benchmark inside the threshold:\n%s", got)
	}
}

// TestBench_PassesInsideThreshold proves that a small change passes, and that a
// geomean line never fails the run.
func TestBench_PassesInsideThreshold(t *testing.T) {
	dir, baseline := fixture(t)
	small := strings.Replace(report, "+25.00%", "+3.00%", 1)
	bench := func(dir string) ([]byte, error) { return []byte("current"), nil }
	compare := func(base, current []byte) ([]byte, error) { return []byte(small), nil }

	var out bytes.Buffer
	if err := check(dir, baseline, 20, bench, compare, &out); err != nil {
		t.Fatalf("check returned %v, want nil:\n%s", err, out.String())
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want empty", out.String())
	}
}

// TestBench_ReadsTheLastDelta proves that the last percentage of a line is the
// one that counts, because benchstat prints the change of each metric in turn.
func TestBench_ReadsTheLastDelta(t *testing.T) {
	line := "BenchmarkX-8   1.240µ ± 1%   1.260µ ± 1%   +1.61% (p=0.002 n=10) +25.00%"
	got := slowLines(line, 20)
	if len(got) != 1 {
		t.Fatalf("slowLines = %v, want the line itself", got)
	}
	if got := slowLines("BenchmarkY-8   -12.00%", 20); len(got) != 0 {
		t.Errorf("slowLines matched a faster benchmark: %v", got)
	}
}
