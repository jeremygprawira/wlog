// Package doctor implements `wlog doctor`: seven checks that answer "is wlog wired up
// here?". A warn alone exits 0, so a pipeline gates on real breakage only.
package doctor

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
	"github.com/jeremygprawira/wlog/cmd/wlog/score"
)

// Check is one doctor result. Status is "pass", "warn", or "fail", and every check carries the
// code it reports under plus why it matters and what to do, so a reader needs no other page.
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Why     string `json:"why"`
	Fix     string `json:"fix"`
}

// Run parses args and runs doctor, returning the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "the module directory")
	jsonOut := flags.Bool("json", false, "print one JSON object per check")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	checks := Inspect(*dir)
	exited := 0
	for _, check := range checks {
		if check.Status == "fail" {
			exited = 1
		}
	}
	if *jsonOut {
		// One document, so a caller parses the report rather than a stream of objects.
		document := report{Version: 1, Passed: exited == 0, Checks: checks}
		data, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog doctor:", err)
			return 2
		}
		_, _ = fmt.Fprintln(stdout, string(data))
		return exited
	}
	for _, check := range checks {
		_, _ = fmt.Fprintf(stdout, "%-5s %-12s %s  %s\n", strings.ToUpper(check.Status), check.Name, check.Code, check.Message)
	}
	return exited
}

// report is the whole --json document: one object holding every check.
type report struct {
	Version int     `json:"version"`
	Passed  bool    `json:"passed"`
	Checks  []Check `json:"checks"`
}

// Inspect runs every check against dir.
func Inspect(dir string) []Check {
	points, pkgs, loadErr := loadPoints(dir)
	checks := []Check{}
	if loadErr != nil {
		// The load failure is reported, and the checks that read files still run: a team that
		// cannot compile today can still learn whether its go.mod and its env are right.
		checks = append(checks, failCheck("load", "WLOG_DOCTOR_LOAD", loadErr.Error(),
			"the rules and the score need the app's syntax and types",
			"fix the package so it compiles, then run doctor again"))
	}
	loaded := loadErr == nil
	checks = append(checks,
		checkModule(dir),
		checkAdapterRequires(dir),
		checkMiddleware(pkgs, points, loaded),
		checkLoggerPerRequest(pkgs, points, loaded),
		checkGlobalLogger(pkgs, points, loaded),
		checkOTelOrder(dir),
		checkDrainEnv(dir),
		checkRedactor(dir),
		checkScore(points, pkgs, loaded),
	)
	return append(checks, adapterChecks(dir)...)
}

// loadPoints loads the module's entry points from dir. A load error is returned, not swallowed:
// a report about a package nobody read is worse than no report.
func loadPoints(dir string) ([]entry.Point, []*packages.Package, error) {
	pkgs, err := entry.LoadDir(dir, "./...")
	if err != nil {
		return nil, nil, fmt.Errorf("load %s/...: %w", dir, err)
	}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, pkgs, fmt.Errorf("load %s/...: %w", dir, pkg.Errors[0])
		}
	}
	return entry.Find(pkgs), pkgs, nil
}

// failCheck, warnCheck, and passCheck build a check with its code, why, and fix. Every check
// carries all three, so a reader learns what was wrong and what to do from one line of output.
func failCheck(name, code, message, why, fix string) Check {
	return Check{Name: name, Status: "fail", Code: code, Message: message, Why: why, Fix: fix}
}

// notChecked builds the check a rule reports when the package did not load: neither a pass it
// never earned nor a failure it cannot see.
func notChecked(name, code, why, fix string) Check {
	return warnCheck(name, code, "not checked: the package did not load", why, fix)
}

// warnCheck builds a check that needs attention but does not fail the run.
func warnCheck(name, code, message, why, fix string) Check {
	return Check{Name: name, Status: "warn", Code: code, Message: message, Why: why, Fix: fix}
}

// passCheck builds a check that passed.
func passCheck(name, code, message, why, fix string) Check {
	return Check{Name: name, Status: "pass", Code: code, Message: message, Why: why, Fix: fix}
}

// checkModule proves go.mod requires wlog, or a workspace provides it.
func checkModule(dir string) Check {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return failCheck("module", "WLOG_DOCTOR_MODULE", "no go.mod in "+dir,
			"doctor reads the module to find the app's handlers and drains",
			"require github.com/jeremygprawira/wlog in go.mod, or open the project inside the workspace")
	}
	if strings.Contains(string(data), "github.com/jeremygprawira/wlog") {
		return passCheck("module", "WLOG_DOCTOR_MODULE", "go.mod requires wlog",
			"doctor reads the module to find the app's handlers and drains",
			"require github.com/jeremygprawira/wlog in go.mod, or open the project inside the workspace")
	}
	if inWorkspace(dir) {
		return passCheck("module", "WLOG_DOCTOR_MODULE", "a go.work provides wlog",
			"doctor reads the module to find the app's handlers and drains",
			"require github.com/jeremygprawira/wlog in go.mod, or open the project inside the workspace")
	}
	return failCheck("module", "WLOG_DOCTOR_MODULE", "go.mod does not require github.com/jeremygprawira/wlog",
		"doctor reads the module to find the app's handlers and drains",
		"require github.com/jeremygprawira/wlog in go.mod, or open the project inside the workspace")
}

// inWorkspace reports whether a go.work above dir belongs to the wlog workspace, so the
// workspace provides the root module and its packages.
func inWorkspace(dir string) bool {
	current, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.work")); err == nil {
			if goMod, err := os.ReadFile(filepath.Join(current, "go.mod")); err == nil &&
				strings.Contains(string(goMod), "module github.com/jeremygprawira/wlog") {
				return true
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}

// adapterModules maps an adapter import to the module that must require it.
var adapterModules = map[string]string{
	"github.com/jeremygprawira/wlog/middleware/echo":  "github.com/jeremygprawira/wlog/middleware/echo",
	"github.com/jeremygprawira/wlog/middleware/echo5": "github.com/jeremygprawira/wlog/middleware/echo5",
	"github.com/jeremygprawira/wlog/middleware/gin":   "github.com/jeremygprawira/wlog/middleware/gin",
	"github.com/jeremygprawira/wlog/errors/herr":      "github.com/jeremygprawira/wlog/errors/herr",
	"github.com/jeremygprawira/wlog/log/zap":          "github.com/jeremygprawira/wlog/log/zap",
	"github.com/jeremygprawira/wlog/log/zerolog":      "github.com/jeremygprawira/wlog/log/zerolog",
	"github.com/jeremygprawira/wlog/log/logrus":       "github.com/jeremygprawira/wlog/log/logrus",
	"github.com/jeremygprawira/wlog/trace/otel":       "github.com/jeremygprawira/wlog/trace/otel",
}

// checkAdapterRequires proves every imported adapter module is required.
func checkAdapterRequires(dir string) Check {
	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return warnCheck("adapters", "WLOG_DOCTOR_ADAPTERS", "no go.mod to check",
			"a module imported but not required breaks the build outside the workspace",
			"add the module to go.mod with go get")
	}
	sources := readSources(dir)
	missing := []string{}
	for adapter, module := range adapterModules {
		if strings.Contains(sources, `"`+adapter+`"`) && !strings.Contains(string(goMod), module) && !inWorkspace(dir) {
			missing = append(missing, module)
		}
	}
	if len(missing) > 0 {
		return failCheck("adapters", "WLOG_DOCTOR_ADAPTERS", "imported but not required: "+strings.Join(missing, ", "),
			"a module imported but not required breaks the build outside the workspace",
			"add the module to go.mod with go get")
	}
	return passCheck("adapters", "WLOG_DOCTOR_ADAPTERS", "every imported adapter is required",
		"a module imported but not required breaks the build outside the workspace",
		"add the module to go.mod with go get")
}

// checkMiddleware proves each package with entry points installs wlog middleware.
func checkMiddleware(pkgs []*packages.Package, points []entry.Point, loaded bool) Check {
	if !loaded {
		return notChecked("middleware", "WLOG_DOCTOR_MIDDLEWARE", "the check reads the app's types", "fix the load error above, then run doctor again")
	}
	if len(points) == 0 {
		return passCheck("middleware", "WLOG_DOCTOR_MIDDLEWARE", "no entry points found",
			"without the middleware no event is opened, so nothing is logged",
			"wrap the router with the adapter's Middleware once, at startup")
	}
	byPackage := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		byPackage[pkg.PkgPath] = pkg
	}
	uncovered := 0
	for _, point := range points {
		pkg := byPackage[point.Package]
		if pkg == nil {
			continue
		}
		if check := findCheck(rules.Evaluate(entry.Program(pkgs), pkg, point, rules.Config{}), rules.RuleMiddleware); !check.Pass {
			uncovered++
		}
	}
	if uncovered > 0 {
		return failCheck("middleware", "WLOG_DOCTOR_MIDDLEWARE", fmt.Sprintf("%d of %d entry points have no wlog middleware", uncovered, len(points)),
			"without the middleware no event is opened, so nothing is logged",
			"wrap the router with the adapter's Middleware once, at startup")
	}
	return passCheck("middleware", "WLOG_DOCTOR_MIDDLEWARE", fmt.Sprintf("all %d entry points are covered", len(points)),
		"without the middleware no event is opened, so nothing is logged",
		"wrap the router with the adapter's Middleware once, at startup")
}

// checkLoggerPerRequest warns when a handler builds a fresh logger per request.
func checkLoggerPerRequest(pkgs []*packages.Package, points []entry.Point, loaded bool) Check {
	if !loaded {
		return notChecked("logger", "WLOG_DOCTOR_LOGGER", "the check reads the app's types", "fix the load error above, then run doctor again")
	}
	byPackage := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		byPackage[pkg.PkgPath] = pkg
	}
	for _, point := range points {
		pkg := byPackage[point.Package]
		if pkg == nil {
			continue
		}
		if handlerBuildsLogger(pkg, point) {
			return warnCheck("logger", "WLOG_DOCTOR_LOGGER", point.Function+" builds a logger inside the handler",
				"a logger built inside a handler cannot batch, flush, or be closed once",
				"build the logger in main and pass it to the middleware")
		}
	}
	return passCheck("logger", "WLOG_DOCTOR_LOGGER", "the middleware receives a process logger",
		"a logger built inside a handler cannot batch, flush, or be closed once",
		"build the logger in main and pass it to the middleware")
}

// checkDrainEnv proves each required drain variable is set or literal.
func checkDrainEnv(dir string) Check {
	sources := readSources(dir)
	var missing []string
	for _, drain := range requiredDrains {
		if !strings.Contains(sources, drain.constructor) {
			continue
		}
		for _, variable := range drain.env {
			if os.Getenv(variable) == "" && !strings.Contains(sources, variable) {
				missing = append(missing, variable)
			}
		}
	}
	if len(missing) > 0 {
		return failCheck("drains", "WLOG_DOCTOR_DRAINS", "missing variables: "+strings.Join(missing, ", "),
			"a drain without its variables disables itself at startup",
			"set the variables in the environment, or drop the drain from the code")
	}
	return passCheck("drains", "WLOG_DOCTOR_DRAINS", "every built drain has its variables",
		"a drain without its variables disables itself at startup",
		"set the variables in the environment, or drop the drain from the code")
}

// checkRedactor proves a redactor is active outside a test.
func checkRedactor(dir string) Check {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return warnCheck("redactor", "WLOG_DOCTOR_REDACTOR", "cannot read "+dir,
			"no redaction means a secret reaches every sink",
			"remove redact.Disabled() outside tests")
	}
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".go") || strings.HasSuffix(item.Name(), "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, item.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(source), "redact.Disabled()") {
			return failCheck("redactor", "WLOG_DOCTOR_REDACTOR", item.Name()+" disables the redactor",
				"no redaction means a secret reaches every sink",
				"remove redact.Disabled() outside tests")
		}
	}
	return passCheck("redactor", "WLOG_DOCTOR_REDACTOR", "the default redactor is active",
		"no redaction means a secret reaches every sink",
		"remove redact.Disabled() outside tests")
}

// checkScore proves the map score clears 60.
func checkScore(points []entry.Point, pkgs []*packages.Package, loaded bool) Check {
	if !loaded {
		return notChecked("score", "WLOG_DOCTOR_SCORE", "the check reads the app's types", "fix the load error above, then run doctor again")
	}
	byPackage := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		byPackage[pkg.PkgPath] = pkg
	}
	checks := make([][]rules.Check, 0, len(points))
	kept := make([]entry.Point, 0, len(points))
	for _, point := range points {
		pkg := byPackage[point.Package]
		if pkg == nil {
			continue
		}
		point.Sensitive = rules.Sensitive(point.Route, nil)
		kept = append(kept, point)
		checks = append(checks, rules.Evaluate(entry.Program(pkgs), pkg, point, rules.Config{}))
	}
	total := score.Total(kept, checks)
	if total < 60 {
		return failCheck("score", "WLOG_DOCTOR_SCORE", fmt.Sprintf("wlog map scores %d, below 60", total),
			"the rules find the observability gaps the map would score",
			"run wlog map and fix the rules it ranks first")
	}
	return passCheck("score", "WLOG_DOCTOR_SCORE", fmt.Sprintf("wlog map scores %d", total),
		"the rules find the observability gaps the map would score",
		"run wlog map and fix the rules it ranks first")
}

// findCheck returns one check by id.
func findCheck(checks []rules.Check, id string) rules.Check {
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	return rules.Check{ID: id}
}

// handlerBuildsLogger reports whether the handler body calls wlog.New.
func handlerBuildsLogger(pkg *packages.Package, point entry.Point) bool {
	var block *ast.BlockStmt
	switch node := point.Node.(type) {
	case *ast.FuncDecl:
		block = node.Body
	case *ast.FuncLit:
		block = node.Body
	}
	if block == nil {
		return false
	}
	found := false
	ast.Inspect(block, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		obj := entry.Callee(pkg, call)
		if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "github.com/jeremygprawira/wlog" && obj.Name() == "New" {
			found = true
			return false
		}
		return true
	})
	return found
}

// drainVars maps a drain constructor substring to its required variables.
type drainVars struct {
	constructor string
	env         []string
}

// requiredDrains lists the drains whose credentials have no default.
var requiredDrains = []drainVars{
	{"axiom.MustNew", []string{"AXIOM_TOKEN", "AXIOM_DATASET"}},
	{"sentry.MustNew", []string{"SENTRY_DSN"}},
	{"webhook.MustNew", []string{"WLOG_WEBHOOK_URL"}},
	{"file.MustNew", []string{"WLOG_FILE_PATH"}},
	{"datadog.MustNew", []string{"DD_API_KEY"}},
	{"posthog.MustNew", []string{"POSTHOG_API_KEY"}},
	{"betterstack.MustNew", []string{"BETTERSTACK_SOURCE_TOKEN"}},
	{"hyperdx.MustNew", []string{"HYPERDX_API_KEY"}},
	{"elastic.MustNew", []string{"ELASTICSEARCH_URL"}},
}

// readSources concatenates every Go file in dir.
func readSources(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var builder strings.Builder
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".go") {
			continue
		}
		if source, err := os.ReadFile(filepath.Join(dir, item.Name())); err == nil {
			builder.Write(source)
		}
	}
	return builder.String()
}
