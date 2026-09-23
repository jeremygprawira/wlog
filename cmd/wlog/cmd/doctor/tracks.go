// This file holds the track A to C additions of wlog doctor: the setup line of every
// installed adapter, and a warning about a package-level log call inside a handler.
package doctor

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// adapterSetups maps every track A to C adapter to the one line that installs it. A
// reader who installs an adapter runs doctor and reads the line for it.
var adapterSetups = map[string]string{
	"github.com/jeremygprawira/wlog/middleware/chi":        "wlogchi.Setup(r)",
	"github.com/jeremygprawira/wlog/middleware/fasthttp":   "wlogfasthttp.Middleware(log)",
	"github.com/jeremygprawira/wlog/middleware/fiber":      "wlogfiber.Setup(app)",
	"github.com/jeremygprawira/wlog/middleware/fiber3":     "wlogfiber3.Setup(app)",
	"github.com/jeremygprawira/wlog/middleware/httprouter": "wloghttprouter.New(log, router)",
	"github.com/jeremygprawira/wlog/middleware/gozero":     "rest.WithRouter(wloggozero.RouterOption(log, inner))",
	"github.com/jeremygprawira/wlog/middleware/hertz":      "wloghertz.Setup(h)",
	"github.com/jeremygprawira/wlog/middleware/kratos":     "khttp.Filter(wlogkratos.Filter(log))",
	"github.com/jeremygprawira/wlog/middleware/huma":       "api.UseMiddleware(wloghuma.Middleware())",
	"github.com/jeremygprawira/wlog/middleware/nethttp":    "wlogstd.Middleware(log)",
	"github.com/jeremygprawira/wlog/middleware/gin":        "wloggin.Middleware(log)",
	"github.com/jeremygprawira/wlog/middleware/echo":       "wlogecho.Middleware(log)",
	"github.com/jeremygprawira/wlog/middleware/echo5":      "wlogecho5.Middleware(log)",
	"github.com/jeremygprawira/wlog/rpc/grpc":              "wloggrpc.ServerOptions(log)",
	"github.com/jeremygprawira/wlog/rpc/connect":           "wlogconnect.Interceptor(log)",
	"github.com/jeremygprawira/wlog/rpc/gqlgen":            "wloggqlgen.Extension()",
	"github.com/jeremygprawira/wlog/rpc/twirp":             "twirp.WithServerHooks(wlogtwirp.ServerHooks())",
	"github.com/jeremygprawira/wlog/client/http":           "wlogclient.Transport(next)",
	"github.com/jeremygprawira/wlog/store/sql":             "sql.OpenDB(wlogsql.Wrap(connector))",
	"github.com/jeremygprawira/wlog/store/pgx":             "wlogpgx.Tracer(next)",
	"github.com/jeremygprawira/wlog/store/gorm":            "db.Use(wloggorm.Plugin())",
	"github.com/jeremygprawira/wlog/store/redis":           "rdb.AddHook(wlogredis.Hook())",
	"github.com/jeremygprawira/wlog/store/mongo":           "opts.SetMonitor(wlogmongo.Monitor(opts.Monitor))",
	"github.com/jeremygprawira/wlog/store/bun":             "db.AddQueryHook(wlogbun.Hook())",
	"github.com/jeremygprawira/wlog/log/slog":              "wlogslog.Handler(next)",
	"github.com/jeremygprawira/wlog/log/logr":              "wlog.WithPlugins(wloglogr.Plugin())",
	"github.com/jeremygprawira/wlog/log/zap":               "wlogzap.Core(next)",
	"github.com/jeremygprawira/wlog/log/zerolog":           "rdb.AddHook? see the package doc",
	"github.com/jeremygprawira/wlog/log/logrus":            "wloglogrus.Install(logger)",
	"github.com/jeremygprawira/wlog/log/hclog":             "wlog.WithPlugins(wloghclog.Plugin(base))",
	"github.com/jeremygprawira/wlog/log/std":               "wlogstdlog.Logger(ctx, prefix, flags)",
	"github.com/jeremygprawira/wlog/drain/elastic":         "PUT _index_template/logs-wlog with elastic.Template(elastic.Elasticsearch)",
}

// adapterChecks returns one check per installed adapter, with its setup line.
func adapterChecks(dir string) []Check {
	sources := readSources(dir)
	checks := []Check{}
	for adapter, setup := range adapterSetups {
		if !strings.Contains(sources, `"`+adapter+`"`) {
			continue
		}
		checks = append(checks, passCheck("adapter", "WLOG_DOCTOR_ADAPTERS",
			adapter+" is installed: "+setup,
			"an installed adapter needs one line at the entry point",
			"install it once, before any route or client call"))
	}
	return checks
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
