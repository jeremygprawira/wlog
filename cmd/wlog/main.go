// Command wlog is the wlog map tool. It finds HTTP handlers, scores their
// observability, writes wlog.map.json, and fails a CI gate when the score drops.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/packages"

	wlogagents "github.com/jeremygprawira/wlog/cmd/wlog/cmd/agents"
	wlogdoctor "github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/internal/term"
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
	minScoreSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "min-score" {
			minScoreSet = true
		}
	})
	patterns := flags.Args()
	if len(patterns) == 0 {
		_, _ = fmt.Fprintln(stderr, "wlog map: at least one package pattern is required")
		return 2
	}

	cfg, code := loadConfig(*configPath, stderr)
	if code != 0 {
		return code
	}
	// A flag wins over the file, including --min-score 0, which turns the gate off. Only a flag
	// the caller did not set may fall back to the config.
	minScore := *minScoreFlag
	if !minScoreSet {
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
	program := entry.Program(pkgs)
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
		checksByPoint = append(checksByPoint, rules.Evaluate(program, pkg, point, cfg))
	}
	points = kept

	// The handler count goes to stderr with the rest of the status lines, and it is the
	// first thing a reader needs when a run reports nothing: zero handlers means the patterns
	// missed the app, not that the app is clean.
	_, _ = fmt.Fprintf(stderr, "%d handlers found\n", len(points))
	if len(points) == 0 {
		_, _ = fmt.Fprintln(stderr, "wlog map: no handlers found; check the patterns")
		return 2
	}

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

	if *outPath != "" && *baselinePath != "" && samePath(*outPath, *baselinePath) {
		// A failing run must never overwrite the baseline it is compared against: the first
		// failure would rewrite the baseline and the next run would pass.
		_, _ = fmt.Fprintln(stderr, "wlog map: --out and --baseline name the same file")
		return 2
	}

	document := report.Build(points, checksByPoint, minScore, gatePass)
	data, err := report.Encode(document)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "wlog map:", err)
		return 2
	}
	// The file is the record of a run that cleared its gate. A failed run writes nothing, so a
	// baseline or a checked-in map can never be overwritten by a regression.
	if *outPath != "" && gatePass {
		if err := os.WriteFile(*outPath, data, 0o644); err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog map:", err)
			return 2
		}
	}

	// --json puts exactly one document on stdout, so a bot can parse it. Every human line, and
	// every status line, goes to stderr, where the JSON cannot be corrupted by one.
	opts := report.TextOptions{Width: term.Width(), Color: term.ColorEnabled()}
	switch {
	case *jsonFlag:
		_, _ = stdout.Write(data)
	case *allFlag:
		_, _ = fmt.Fprint(stderr, report.Matrix(document, opts))
	case *entryFlag != "":
		detail, found := report.Entry(document, *entryFlag, opts)
		if !found {
			_, _ = fmt.Fprintf(stderr, "wlog map: no entry point named %q\n", *entryFlag)
			return 2
		}
		_, _ = fmt.Fprint(stderr, detail)
	default:
		_, _ = fmt.Fprint(stderr, report.Summary(document, opts))
	}
	if !gatePass {
		_, _ = fmt.Fprintln(stderr, "wlog map: gate failed")
		return 1
	}
	return 0
}

// samePath reports whether two paths name the same file, resolving each one so ./x and x agree.
func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return absA == absB
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
