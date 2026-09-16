// Command wlog is the wlog map tool. It finds HTTP handlers, scores their
// observability, writes wlog.map.json, and fails a CI gate when the score drops.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/report"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
	"github.com/jeremygprawira/wlog/cmd/wlog/score"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process exit, so a test can call it. It returns the exit
// code: 0 pass, 1 gate failure, 2 load or config error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "map" {
		fmt.Fprintln(stderr, "usage: wlog map [flags] <package patterns...>")
		return 2
	}

	flags := flag.NewFlagSet("map", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outPath := flags.String("out", "wlog.map.json", "write the map to this file")
	minScoreFlag := flags.Int("min-score", 0, "exit non-zero when the score is below this")
	baselinePath := flags.String("baseline", "", "exit non-zero when the score is below the score in this file")
	configPath := flags.String("config", "", "rules config file")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	patterns := flags.Args()
	if len(patterns) == 0 {
		fmt.Fprintln(stderr, "wlog map: at least one package pattern is required")
		return 2
	}

	cfg, code := loadConfig(*configPath, stderr)
	if code != 0 {
		return code
	}
	minScore := *minScoreFlag
	if minScore == 0 {
		minScore = cfg.MinScore
	}

	pkgs, err := entry.Load(patterns...)
	if err != nil {
		fmt.Fprintln(stderr, "wlog map:", err)
		return 2
	}
	if !reportLoadErrors(pkgs, stderr) {
		return 2
	}

	points := entry.Find(pkgs)
	byPath := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		byPath[pkg.PkgPath] = pkg
	}
	kept := make([]entry.Point, 0, len(points))
	checksByPoint := make([][]rules.Check, 0, len(points))
	for _, point := range points {
		pkg := byPath[point.Package]
		if pkg == nil {
			continue
		}
		point.Sensitive = rules.Sensitive(point.Route, cfg.SensitivePatterns)
		kept = append(kept, point)
		checksByPoint = append(checksByPoint, rules.Evaluate(pkg, point, cfg))
	}
	points = kept

	total := score.Total(checksByPoint)
	gatePass := minScore == 0 || total >= minScore
	if *baselinePath != "" {
		baseline, err := readBaseline(*baselinePath)
		if err != nil {
			fmt.Fprintln(stderr, "wlog map:", err)
			return 2
		}
		if total < baseline.Score {
			gatePass = false
		}
	}

	document := report.Build(points, checksByPoint, minScore, gatePass)
	data, err := report.Encode(document)
	if err != nil {
		fmt.Fprintln(stderr, "wlog map:", err)
		return 2
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, data, 0o644); err != nil {
			fmt.Fprintln(stderr, "wlog map:", err)
			return 2
		}
	}

	fmt.Fprintf(stdout, "wlog map: score %d\n", document.Score)
	for _, fix := range document.TopFixes {
		fmt.Fprintln(stdout, "  fix:", fix)
	}
	if !gatePass {
		fmt.Fprintln(stdout, "wlog map: gate failed")
		return 1
	}
	return 0
}

// loadConfig reads an explicit config file, or finds a default one in the working
// directory.
func loadConfig(explicit string, stderr io.Writer) (rules.Config, int) {
	path := explicit
	if path == "" {
		path = rules.FindConfig(".")
	}
	if path == "" {
		return rules.Config{}, 0
	}
	cfg, err := rules.LoadConfig(path)
	if err != nil {
		fmt.Fprintln(stderr, "wlog map:", err)
		return rules.Config{}, 2
	}
	return cfg, 0
}

// reportLoadErrors prints any package load error and reports whether the load is clean.
func reportLoadErrors(pkgs []*packages.Package, stderr io.Writer) bool {
	clean := true
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			fmt.Fprintln(stderr, "wlog map:", err)
			clean = false
		}
	}
	return clean
}

// readBaseline reads a previous wlog.map.json.
func readBaseline(path string) (report.Map, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return report.Map{}, err
	}
	var baseline report.Map
	if err := json.Unmarshal(data, &baseline); err != nil {
		return report.Map{}, fmt.Errorf("baseline %s: %w", path, err)
	}
	return baseline, nil
}
