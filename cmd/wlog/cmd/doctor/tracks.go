// This file holds the track A to C additions of wlog doctor: the setup line of every
// installed adapter, and a warning about a package-level log call inside a handler.
package doctor

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/internal/adapters"
)

// elasticSetup is the install line for the Elastic drain. A drain installs through the
// environment, so it is not a row in the adapter table.
const elasticSetup = "PUT _index_template/logs-wlog with elastic.Template(elastic.Elasticsearch)"

// adapterChecks returns one check per installed adapter, in table order, with its setup line.
func adapterChecks(dir string) []Check {
	sources := readSources(dir)
	checks := []Check{}
	for _, adapter := range adapters.Table {
		if adapter.Setup == "" || !strings.Contains(sources, `"`+adapter.Wlog+`"`) {
			continue
		}
		checks = append(checks, adapterLine(adapter.Wlog, adapter.Setup))
	}
	const elastic = "github.com/jeremygprawira/wlog/drain/elastic"
	if strings.Contains(sources, `"`+elastic+`"`) {
		checks = append(checks, adapterLine(elastic, elasticSetup))
	}
	return checks
}

// adapterLine builds the doctor line for one installed adapter.
func adapterLine(path, setup string) Check {
	return passCheck("adapter", "WLOG_DOCTOR_ADAPTERS",
		path+" is installed: "+setup,
		"an installed adapter needs one line at the entry point",
		"install it once, before any route or client call")
}

// checkGlobalLogger warns about a package-level log call inside a handler, because that
// line lands outside the event and a reader cannot find it.
func checkGlobalLogger(pkgs []*packages.Package, points []entry.Point, loaded bool) Check {
	if !loaded {
		return notChecked("global", "WLOG_DOCTOR_LOGGER", "the check reads the app's types",
			"fix the load error above, then run doctor again")
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
		for _, file := range pkg.Syntax {
			if packageLogCall(file, point.Function) {
				return warnCheck("global", "WLOG_DOCTOR_LOGGER",
					point.Function+" calls a package-level logger",
					"a package-level log line lands outside the event, so a reader cannot find it",
					"write the line on the request context with wlog.Info, wlog.Warn, or wlog.Error")
			}
		}
	}
	return passCheck("global", "WLOG_DOCTOR_LOGGER", "no handler calls a package-level logger",
		"a package-level log line lands outside the event, so a reader cannot find it",
		"write the line on the request context with wlog.Info, wlog.Warn, or wlog.Error")
}

// packageLogCall reports whether one function body calls a package-level logging
// function, such as slog.Info or log.Print.
func packageLogCall(file *ast.File, function string) bool {
	found := false
	for _, decl := range file.Decls {
		declaration, ok := decl.(*ast.FuncDecl)
		if !ok || declaration.Name.Name != function || declaration.Body == nil {
			continue
		}
		ast.Inspect(declaration.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if isPackageLogger(pkg.Name) && isLogFunction(selector.Sel.Name) {
				found = true
				return false
			}
			return true
		})
	}
	return found
}

// isPackageLogger names the packages whose global functions write outside the event.
func isPackageLogger(name string) bool {
	return name == "log" || name == "slog" || name == "fmt"
}

// isLogFunction names the logging functions of those packages. The slog Context
// variants are the correct call, so they are not here.
func isLogFunction(name string) bool {
	switch name {
	case "Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln",
		"Info", "Warn", "Error", "Debug":
		return true
	}
	return false
}
