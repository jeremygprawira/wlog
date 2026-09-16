package init

import (
	"fmt"
	"strings"
)

// setupFor renders the generated wlog.go and .env.example for one framework and drain.
func setupFor(framework, drain, module, pkg string) (setup, error) {
	drainImport, drainExpr, env := drainFor(drain)

	var imports []string
	var middlewareImport, middlewareFunc string
	switch framework {
	case "nethttp", "mux":
		imports = append(imports, `"net/http"`)
		middlewareImport = `wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"`
		middlewareFunc = `// WrapHandler covers every route with one wide event per request.
func WrapHandler(next http.Handler) http.Handler {
	return wlogstd.Middleware(NewLogger())(next)
}`
	case "echo":
		imports = append(imports, `"github.com/labstack/echo/v4"`)
		middlewareImport = `wlogecho "github.com/jeremygprawira/wlog/middleware/echo"`
		middlewareFunc = `// LoggerMiddleware covers every route with one wide event per request.
func LoggerMiddleware() echo.MiddlewareFunc {
	return wlogecho.Middleware(NewLogger())
}`
	case "echo5":
		imports = append(imports, `echo "github.com/labstack/echo/v5"`)
		middlewareImport = `wlogecho5 "github.com/jeremygprawira/wlog/middleware/echo5"`
		middlewareFunc = `// LoggerMiddleware covers every route with one wide event per request.
func LoggerMiddleware() echo.MiddlewareFunc {
	return wlogecho5.Middleware(NewLogger())
}`
	case "gin":
		imports = append(imports, `"github.com/gin-gonic/gin"`)
		middlewareImport = `wloggin "github.com/jeremygprawira/wlog/middleware/gin"`
		middlewareFunc = `// LoggerMiddleware covers every route with one wide event per request.
func LoggerMiddleware() gin.HandlerFunc {
	return wloggin.Middleware(NewLogger())
}`
	default:
		return setup{}, fmt.Errorf("unknown framework %q", framework)
	}
	imports = append(imports, `"github.com/jeremygprawira/wlog"`)
	if drainImport != "" {
		imports = append(imports, drainImport)
	}
	imports = append(imports, middlewareImport)

	var builder strings.Builder
	fmt.Fprintf(&builder, "package %s\n\nimport (\n", pkg)
	for _, line := range imports {
		fmt.Fprintf(&builder, "\t%s\n", line)
	}
	builder.WriteString(")\n\n")
	builder.WriteString("// NewLogger builds this service's logger: one wide event per request.\n")
	builder.WriteString("func NewLogger() *wlog.Logger {\n")
	fmt.Fprintf(&builder, "\treturn wlog.New(\n\t\twlog.WithService(%q, \"0.0.1\", \"local\"),\n", module)
	drainLine := "\t\t// no drain yet: events go to stdout. Add one here.\n"
	if drainExpr != "" {
		drainLine = fmt.Sprintf("\t\twlog.WithDrains(%s),\n", drainExpr)
	}
	builder.WriteString(drainLine)
	builder.WriteString("\t)\n}\n\n")
	builder.WriteString(middlewareFunc)
	builder.WriteString("\n")

	return setup{wlogGo: builder.String(), envExample: env}, nil
}

// drainFor returns the import, the expression, and the env example for one drain.
func drainFor(drain string) (importLine, expression, envExample string) {
	switch drain {
	case "", "stdout":
		return "", "", "# No drain variables yet. Events go to stdout.\n"
	case "axiom":
		return "\"github.com/jeremygprawira/wlog/drain/axiom\"\n\t\"github.com/jeremygprawira/wlog/pipeline\"", "pipeline.Wrap(axiom.MustNew())",
			"AXIOM_TOKEN=\nAXIOM_DATASET=\n"
	case "loki":
		return "\"github.com/jeremygprawira/wlog/drain/loki\"\n\t\"github.com/jeremygprawira/wlog/pipeline\"", "pipeline.Wrap(loki.MustNew())",
			"LOKI_URL=http://localhost:3100\n"
	case "file":
		return "\"github.com/jeremygprawira/wlog/drain/file\"\n\t\"github.com/jeremygprawira/wlog/pipeline\"", "pipeline.Wrap(file.MustNew())",
			"WLOG_FILE_PATH=.logs/events.ndjson\n"
	default:
		return "", "", ""
	}
}
