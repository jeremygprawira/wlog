// Command wlog is the wlog map tool. It finds HTTP handlers, scores their
// observability, writes wlog.map.json, and fails a CI gate when the score drops.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	wlogagents "github.com/jeremygprawira/wlog/cmd/wlog/cmd/agents"
	wlogdoctor "github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
	wlogexplain "github.com/jeremygprawira/wlog/cmd/wlog/cmd/explain"
	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
	wlogmcp "github.com/jeremygprawira/wlog/cmd/wlog/cmd/mcp"
	wlogquery "github.com/jeremygprawira/wlog/cmd/wlog/cmd/query"
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
	if len(args) > 0 && args[0] == "query" {
		return wlogquery.Run(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "tail" {
		return wlogquery.Run(append([]string{"--follow"}, args[1:]...), stdout, stderr)
	}
	if len(args) > 0 && args[0] == "mcp" {
		return wlogmcp.Run(args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "explain" || args[0] == "rules" || args[0] == "schema" || args[0] == "version") {
		return wlogexplain.Run(args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		_, _ = fmt.Fprint(stdout, usage())
		return 0
	}
	if len(args) == 0 || args[0] != "map" {
		_, _ = fmt.Fprint(stderr, usage())
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
	noWrite := flags.Bool("no-write", false, "do not write the output file")
	format := flags.String("format", "", "output format for stdout: empty for text, or sarif")
	strictFlag := flags.Bool("strict", false, "also fail on a per-rule regression")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *strictFlag && *baselinePath == "" {
		// --strict compares against a baseline, so it has nothing to do without one.
		_, _ = fmt.Fprintln(stderr, "wlog map: --strict needs --baseline")
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
		baseline, err := readBaselineAt(*baselinePath, *outPath)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog map:", err)
			return 2
		}
		_, _ = fmt.Fprintf(stderr, "wlog map: baseline %s scored %d\n", *baselinePath, baseline.Score)
		if total < baseline.Score {
			gatePass = false
		}
		if *strictFlag {
			// Per handler AND per rule: summing per rule let one handler be fixed while another
			// broke, which is a regression the total score hides.
			for _, regression := range compareStrict(points, checksByPoint, baseline) {
				_, _ = fmt.Fprintf(stderr, "wlog map: %s\n", regression)
				gatePass = false
			}
		}
	}

	if *outPath != "" && *baselinePath != "" && !strings.HasPrefix(*baselinePath, "git:") && samePath(*outPath, *baselinePath) {
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
	if *outPath != "" && gatePass && !*noWrite {
		if err := os.WriteFile(*outPath, data, 0o644); err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog map:", err)
			return 2
		}
	}

	// --json puts exactly one document on stdout, so a bot can parse it. Every human line, and
	// every status line, goes to stderr, where the JSON cannot be corrupted by one.
	opts := report.TextOptions{Width: term.Width(), Color: term.ColorEnabled()}
	switch {
	case *format == "sarif":
		sarifData, err := report.SARIF(document)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog map:", err)
			return 2
		}
		_, _ = stdout.Write(sarifData)
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
		_, _ = fmt.Fprint(stderr, report.Text(document, opts))
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

// usage lists every command and the exit codes, so a reader who types wlog --help learns both.
func usage() string {
	return `wlog: observability tooling for a Go service.

usage: wlog <command> [flags] [arguments]

commands:
  map     score every HTTP handler against the wlog rules, and write wlog.map.json
  init    scaffold wlog into a project
  doctor  report on the wlog setup of a project
  agents  write agent instructions and skills
  help    print this text

exit codes:
  0  the run passed every gate
  1  a gate failed: the score is below --min-score, or below --baseline
  2  the run could not start: a bad flag, a missing pattern, an unreadable config, a load error
`
}

// compareStrict reports each handler and rule pair that passed at the baseline and fails now.
func compareStrict(points []entry.Point, byHandler [][]rules.Check, baseline report.Map) []string {
	type key struct{ handler, rule string }
	wasPassing := map[key]bool{}
	for _, handler := range baseline.Handlers {
		name := handler.Function + "|" + handler.Route
		for _, check := range handler.Checks {
			if check.Pass {
				wasPassing[key{name, check.ID}] = true
			}
		}
	}

	var regressions []string
	for i, point := range points {
		name := point.Function + "|" + point.Route
		for _, check := range byHandler[i] {
			if !check.Applicable || check.Pass {
				continue
			}
			if !wasPassing[key{name, check.ID}] {
				continue
			}
			regressions = append(regressions, fmt.Sprintf("%s:%d %s.%s regressed on %s",
				point.File, point.Line, point.Function, check.ID, check.ID))
		}
	}
	sort.Strings(regressions)
	return regressions
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
// readBaselineAt reads the baseline a run compares against.
//
// A path of the form git:<ref> reads the map this run would write, at that revision, through git
// show, so a comparison needs no copy of the file on disk and cannot be moved by the run itself.
func readBaselineAt(spec, outPath string) (report.Map, error) {
	ref, isGit := strings.CutPrefix(spec, "git:")
	if !isGit {
		return readBaseline(spec)
	}
	name := repoRelative(outPath)
	out, err := exec.CommandContext(context.Background(), "git", "show", ref+":"+name).Output()
	if err != nil {
		return report.Map{}, fmt.Errorf("baseline git:%s: %w", ref, err)
	}
	var baseline report.Map
	if err := json.Unmarshal(out, &baseline); err != nil {
		return report.Map{}, fmt.Errorf("baseline git:%s:%s: %w", ref, name, err)
	}
	return baseline, nil
}

// repoRelative turns the output path into the path git names it by. git reads a path in
// <ref>:<path> from the repository root, however deep in the tree the command runs.
func repoRelative(path string) string {
	if path == "" {
		return "wlog.map.json"
	}
	root, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return path
	}
	top := strings.TrimSpace(string(root))
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(top, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// readBaseline reads one baseline file from disk.
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
