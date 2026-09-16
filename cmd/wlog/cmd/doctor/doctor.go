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

// Check is one doctor result. Status is "pass", "warn", or "fail".
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
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
		if *jsonOut {
			data, _ := json.Marshal(check)
			_, _ = fmt.Fprintln(stdout, string(data))
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%-5s %-12s %s\n", strings.ToUpper(check.Status), check.Name, check.Message)
	}
	return exited
}

// Inspect runs every check against dir.
func Inspect(dir string) []Check {
	points, pkgs := loadPoints(dir)
	return []Check{
		checkModule(dir),
		checkAdapterRequires(dir),
		checkMiddleware(pkgs, points),
		checkLoggerPerRequest(pkgs, points),
		checkDrainEnv(dir),
		checkRedactor(dir),
		checkScore(points, pkgs),
	}
}

// loadPoints loads the module's entry points, quietly.
func loadPoints(dir string) ([]entry.Point, []*packages.Package) {
	pkgs, err := entry.Load(dir + "/...")
	if err != nil {
		return nil, nil
	}
	return entry.Find(pkgs), pkgs
}

// checkModule proves go.mod requires wlog, or a workspace provides it.
func checkModule(dir string) Check {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return Check{"module", "fail", "no go.mod in " + dir}
	}
	if strings.Contains(string(data), "github.com/jeremygprawira/wlog") {
		return Check{"module", "pass", "go.mod requires wlog"}
	}
	if inWorkspace(dir) {
		return Check{"module", "pass", "a go.work provides wlog"}
	}
	return Check{"module", "fail", "go.mod does not require github.com/jeremygprawira/wlog"}
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
		return Check{"adapters", "warn", "no go.mod to check"}
	}
	sources := readSources(dir)
	missing := []string{}
	for adapter, module := range adapterModules {
		if strings.Contains(sources, `"`+adapter+`"`) && !strings.Contains(string(goMod), module) && !inWorkspace(dir) {
			missing = append(missing, module)
		}
	}
	if len(missing) > 0 {
		return Check{"adapters", "fail", "imported but not required: " + strings.Join(missing, ", ")}
	}
	return Check{"adapters", "pass", "every imported adapter is required"}
}

// checkMiddleware proves each package with entry points installs wlog middleware.
func checkMiddleware(pkgs []*packages.Package, points []entry.Point) Check {
	if len(points) == 0 {
		return Check{"middleware", "pass", "no entry points found"}
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
		if check := findCheck(rules.Evaluate(pkg, point, rules.Config{}), rules.RuleMiddleware); !check.Pass {
			uncovered++
		}
	}
	if uncovered > 0 {
		return Check{"middleware", "fail", fmt.Sprintf("%d of %d entry points have no wlog middleware", uncovered, len(points))}
	}
	return Check{"middleware", "pass", fmt.Sprintf("all %d entry points are covered", len(points))}
}

// checkLoggerPerRequest warns when a handler builds a fresh logger per request.
func checkLoggerPerRequest(pkgs []*packages.Package, points []entry.Point) Check {
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
			return Check{"logger", "warn", point.Function + " builds a logger inside the handler"}
		}
	}
	return Check{"logger", "pass", "the middleware receives a process logger"}
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
		return Check{"drains", "fail", "missing variables: " + strings.Join(missing, ", ")}
	}
	return Check{"drains", "pass", "every built drain has its variables"}
}

// checkRedactor proves a redactor is active outside a test.
func checkRedactor(dir string) Check {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Check{"redactor", "warn", "cannot read " + dir}
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
			return Check{"redactor", "fail", item.Name() + " disables the redactor"}
		}
	}
	return Check{"redactor", "pass", "the default redactor is active"}
}

// checkScore proves the map score clears 60.
func checkScore(points []entry.Point, pkgs []*packages.Package) Check {
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
		checks = append(checks, rules.Evaluate(pkg, point, rules.Config{}))
	}
	total := score.Total(kept, checks)
	if total < 60 {
		return Check{"score", "fail", fmt.Sprintf("wlog map scores %d, below 60", total)}
	}
	return Check{"score", "pass", fmt.Sprintf("wlog map scores %d", total)}
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
	{"posthog.Must", []string{"POSTHOG_API_KEY"}},
	{"betterstack.Must", []string{"BETTERSTACK_SOURCE_TOKEN"}},
	{"hyperdx.Must", []string{"HYPERDX_API_KEY"}},
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
