// Command bench compares the benchmarks of the root module with a baseline, and
// fails on a slowdown over a threshold.
//
// A performance budget needs two numbers: the run that a change produces, and
// the run that the repository agreed to keep. The command takes the baseline
// from bench/baseline.txt, runs every benchmark with -count=10, and asks
// benchstat for the difference. A benchmark that is slower by more than the
// threshold fails the run.
//
// The go command measures one machine, so the command is for a change and not
// for a fact: a move of a few percent on a busy laptop means nothing. The
// threshold is 20% by default, which is wide enough to survive that noise.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// defaultBaseline is the file that holds the agreed numbers.
const defaultBaseline = "bench/baseline.txt"

// delta matches the last percentage of a benchstat line, with its sign.
var delta = regexp.MustCompile(`([+-][\d.]+)%`)

// main finds the workspace root, then compares the benchmarks.
func main() {
	baseline := flag.String("baseline", defaultBaseline, "the file that holds the baseline")
	maxSlowdown := flag.Float64("max-slowdown", 20, "the largest slowdown in percent that passes")
	pkg := flag.String("pkg", "./...", "the package pattern to benchmark")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	path := *baseline
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if err := check(root, path, *maxSlowdown, benchRunner(*pkg), runBenchstat, os.Stdout); err != nil {
		fail(err)
	}
}

// benchRunner returns the benchmark run of one package pattern.
func benchRunner(pkg string) func(dir string) ([]byte, error) {
	return func(dir string) ([]byte, error) { return runBench(dir, pkg) }
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "bench:", err)
	os.Exit(1)
}

// check compares the current benchmarks with the baseline and prints one problem
// per benchmark over the threshold.
//
// Both the benchmark run and the comparison are parameters, so a test proves the
// threshold and the report without a benchmark run and without benchstat.
func check(root, baseline string, maxSlowdown float64, bench func(dir string) ([]byte, error), compare func(baseline, current []byte) ([]byte, error), out io.Writer) error {
	base, err := os.ReadFile(baseline)
	if err != nil {
		return fmt.Errorf("read the baseline: %w", err)
	}
	current, err := bench(root)
	if err != nil {
		return err
	}

	report, err := compare(base, current)
	if err != nil {
		return err
	}

	bad := 0
	for _, line := range slowLines(string(report), maxSlowdown) {
		bad++
		if _, err := fmt.Fprintf(out, "%s: BENCH1: the benchmark is slower than the baseline\n", line); err != nil {
			return err
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d benchmark(s) are more than %.0f%% slower", bad, maxSlowdown)
	}
	return nil
}

// slowLines returns the benchstat lines whose slowdown passes the threshold.
//
// A benchstat line names a benchmark and ends with the change against the
// baseline, as in "± 1%  +25.00% (p=0.000 n=10)". The last percentage is the
// change for the metric of that table.
func slowLines(report string, maxSlowdown float64) []string {
	var out []string
	for _, line := range strings.Split(report, "\n") {
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}
		all := delta.FindAllStringSubmatch(line, -1)
		if len(all) == 0 {
			continue
		}
		last := all[len(all)-1][1]
		value, err := strconv.ParseFloat(strings.TrimPrefix(last, "+"), 64)
		if err != nil || !strings.HasPrefix(last, "+") {
			continue
		}
		if value > maxSlowdown {
			out = append(out, line)
		}
	}
	return out
}

// runBench runs the benchmarks of one package pattern ten times.
//
// Ten runs give benchstat enough numbers to report a change that is real rather
// than the noise of one measurement.
func runBench(dir, pkg string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "go", "test", "-run=xxx", "-bench=.", "-benchmem", "-count=10", pkg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	return cmd.CombinedOutput()
}

// runBenchstat compares two benchmark files with benchstat.
func runBenchstat(baseline, current []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "wlog-bench-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(oldPath, baseline, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(newPath, current, 0o600); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(context.Background(), "go", "run", "golang.org/x/perf/cmd/benchstat", oldPath, newPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("benchstat: %w\n%s", err, out)
	}
	return out, nil
}
