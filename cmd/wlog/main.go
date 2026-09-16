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

	wlogagents "github.com/jeremygprawira/wlog/cmd/wlog/cmd/agents"
	wlogdoctor "github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
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
	if len(args) > 0 && args[0] == "init" {
		return wloginit.Run(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "doctor" {
		return wlogdoctor.Run(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "agents" {
		return wlogagents.Run(args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "map" {
		_, _ = fmt.Fprintln(stderr, "usage: wlog map [flags] <package patterns...>")
		return 2
	}

	flags := flag.NewFlagSet("map", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outPath := flags.String("out", "wlog.map.json", "write the map to this file")
	minScoreFlag := flags.Int("min-score", 0, "exit non-zero when the score is below this")
	baselinePath := flags.String("baseline", "", "exit non-zero when the score is below the score in this file")
	configPath := flags.String("config", "", "rules config file")
	allFlag := flags.Bool("all", false, "print the check matrix")
	entryFlag := flags.String("entry", "", "print the full detail for one entry point")
	jsonFlag := flags.Bool("json", false, "print the report as JSON")
	strictFlag := flags.Bool("strict", false, "also fail on a per-rule regression")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	patterns := flags.Args()
	if len(patterns) == 0 {
		_, _ = fmt.Fprintln(stderr, "wlog map: at least one package pattern is required")
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
		_, _ = fmt.Fprintln(stderr, "wlog map:", err)
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

	total := score.Total(points, checksByPoint)
	gatePass := minScore == 0 || total >= minScore
	if *baselinePath != "" {
		baseline, err := readBaseline(*baselinePath)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog map:", err)
			return 2
		}
		if total < baseline.Score {
			gatePass = false
		}
		if *strictFlag {
			currentPoints := perRulePoints(checksByPoint)
			baselinePoints := perRulePointsOfMap(baseline)
			for rule, points := range currentPoints {
				if points > baselinePoints[rule] {
					gatePass = false
				}
			}
		}
	}

	document := report.Build(points, checksByPoint, minScore, gatePass)
	data, err := report.Encode(document)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "wlog map:", err)
		return 2
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, data, 0o644); err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog map:", err)
			return 2
		}
	}

	switch {
	case *jsonFlag:
		_, _ = stdout.Write(data)
	case *allFlag:
		_, _ = fmt.Fprint(stdout, report.Matrix(document))
	case *entryFlag != "":
		detail, found := report.Entry(document, *entryFlag)
		if !found {
			_, _ = fmt.Fprintf(stderr, "wlog map: no entry point named %q\n", *entryFlag)
			return 2
		}
		_, _ = fmt.Fprint(stdout, detail)
	default:
		_, _ = fmt.Fprintf(stdout, "wlog map: score %d (%s)\n", document.Score, document.Grade)
		for _, fix := range document.TopFixes {
			_, _ = fmt.Fprintln(stdout, "  fix:", fix)
		}
	}
	if !gatePass {
		_, _ = fmt.Fprintln(stdout, "wlog map: gate failed")
		return 1
	}
	return 0
}

// perRulePoints sums the failed weight per rule across the handlers.
func perRulePoints(byHandler [][]rules.Check) map[string]int {
	points := map[string]int{}
	for _, checks := range byHandler {
		for _, check := range checks {
			if !check.Pass && check.Weight > 0 {
				points[check.ID] += check.Weight
			}
		}
	}
	return points
}

// perRulePointsOfMap reads the same totals from a previous map file.
func perRulePointsOfMap(m report.Map) map[string]int {
	points := map[string]int{}
	for _, handler := range m.Handlers {
		for _, check := range handler.Checks {
			if !check.Pass && check.Weight > 0 {
				points[check.ID] += check.Weight
			}
		}
	}
	return points
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
		_, _ = fmt.Fprintln(stderr, "wlog map:", err)
		return rules.Config{}, 2
	}
	return cfg, 0
}

// reportLoadErrors prints any package load error and reports whether the load is clean.
func reportLoadErrors(pkgs []*packages.Package, stderr io.Writer) bool {
	clean := true
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			_, _ = fmt.Fprintln(stderr, "wlog map:", err)
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
