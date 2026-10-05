// Command cover measures the statement coverage of every package of the root
// module, and fails a package below the minimum.
//
// A coverage floor is only honest when it rises. The command keeps the packages
// that sit under the minimum today in tools/cover-known-low.txt, so a package
// may stay where it is while a task raises it later, and a package that falls
// below the minimum fails the run.
//
// The command reads the output of `go test -cover ./...` of the root module,
// because that module holds the core, the drains, and the middleware. The
// sub-modules are mostly adapters whose tests live in their own module.
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
	"sort"
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// knownLowPath is the list of packages that may sit under the minimum today.
const knownLowPath = "tools/cover-known-low.txt"

// modulesPath lists the own modules the check also measures, one directory per line.
// The root module holds the core, the drains, and the middleware, and these hold the
// phase 14 adapters whose tests live in their own module.
const modulesPath = "tools/cover-modules.txt"

// coverageLine matches one line of the go test output and captures the package
// and its coverage. The line holds an elapsed time, or the word cached, between
// the package and the coverage.
var coverageLine = regexp.MustCompile(`^(?:ok|\?)\s+(\S+)\s+(?:\(cached\)\s+|\S+\s+)?coverage: ([\d.]+)% of statements`)

// main finds the workspace root and exits 1 when a package is under the minimum.
func main() {
	min := flag.Float64("min", 85, "the lowest coverage a package may have")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	if err := check(root, *min, runCover, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "cover:", err)
	os.Exit(1)
}

// result is the coverage of one package.
type result struct {
	pkg     string
	percent float64
}

// check reads the coverage of the root module and prints one problem per package
// under the minimum that the known-low list does not hold.
//
// run is a parameter, so a test proves the parse and the ratchet without a test
// run.
func check(root string, min float64, run func(dir string) ([]byte, error), out io.Writer) error {
	knownLow, err := readList(filepath.Join(root, knownLowPath))
	if err != nil {
		return err
	}
	results := []result{}
	text, err := run(root)
	if err != nil {
		return err
	}
	results = append(results, parseCoverage(string(text))...)

	// The own modules the list names are measured too, so a new module is visible to the
	// gate and a later fall fails it.
	dirs, err := readModules(filepath.Join(root, modulesPath))
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		text, err := run(filepath.Join(root, dir))
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		results = append(results, parseCoverage(string(text))...)
	}
	if len(results) == 0 {
		return fmt.Errorf("the go test output holds no coverage line")
	}

	// A package on the list is a task for later, so it does not fail the run. A
	// package that falls under the minimum without a line on the list does.
	under, unlisted := 0, 0
	for _, r := range results {
		if r.percent >= min {
			continue
		}
		under++
		if knownLow[r.pkg] {
			continue
		}
		unlisted++
		if _, err := fmt.Fprintf(out, "%s: COV1: coverage %.1f%% is under %.1f%%\n", r.pkg, r.percent, min); err != nil {
			return err
		}
	}
	if unlisted > 0 {
		return fmt.Errorf("%d package(s) are under %.1f%%", unlisted, min)
	}
	if under == 0 {
		return nil
	}
	return nil
}

// readModules reads the module directories the check measures. A missing list is not an
// error, so an older tree still runs.
func readModules(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		dirs = append(dirs, line)
	}
	return dirs, nil
}

// parseCoverage reads the coverage of every package that the go command reports.
func parseCoverage(text string) []result {
	var results []result
	for _, line := range strings.Split(text, "\n") {
		m := coverageLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		percent, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		results = append(results, result{pkg: m[1], percent: percent})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].pkg < results[j].pkg })
	return results
}

// runCover runs `go test -cover ./...` in the root module.
func runCover(dir string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "go", "test", "-cover", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	return cmd.CombinedOutput()
}

// readList reads a list of paths, one per line. A missing list is not an error.
func readList(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	list := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line != "" {
			list[line] = true
		}
	}
	return list, nil
}
