package init

import (
	"fmt"
	"go/format"
	"strings"
)

// setupFor renders the generated wlog_setup.go and .env.example for one framework and drain.
//
// The generated logger reads its drains and its identity from the environment through
// setup.FromEnv, so a deployment changes no code. The drain flag only seeds .env.example.
func setupFor(framework, drain, module, pkg string) (setup, error) {
	env := drainFor(drain)

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
	case "chi":
		imports = append(imports, `"net/http"`)
		middlewareImport = `wlogchi "github.com/jeremygprawira/wlog/middleware/chi"`
		middlewareFunc = `// LoggerMiddleware covers every route with one wide event per request.
func LoggerMiddleware() func(http.Handler) http.Handler {
	return wlogchi.Middleware(NewLogger())
}`
	case "fiber":
		imports = append(imports, `"github.com/gofiber/fiber/v2"`)
		middlewareImport = `wlogfiber "github.com/jeremygprawira/wlog/middleware/fiber"`
		middlewareFunc = `// LoggerMiddleware covers every route with one wide event per request.
func LoggerMiddleware() fiber.Handler {
	return wlogfiber.Middleware(NewLogger())
}`
	case "fiber3":
		imports = append(imports, `"github.com/gofiber/fiber/v3"`)
		middlewareImport = `wlogfiber3 "github.com/jeremygprawira/wlog/middleware/fiber3"`
		middlewareFunc = `// LoggerMiddleware covers every route with one wide event per request.
func LoggerMiddleware() fiber.Handler {
	return wlogfiber3.Middleware(NewLogger())
}`
	case "":
		// A module with no HTTP framework, such as a worker or a CLI, gets the logger and no
		// middleware. Its entry point installs its own adapter.
	default:
		return setup{}, fmt.Errorf("unknown framework %q", framework)
	}
	imports = append(imports, `"github.com/jeremygprawira/wlog"`)
	imports = append(imports, `"github.com/jeremygprawira/wlog/setup"`)
	if middlewareImport != "" {
		imports = append(imports, middlewareImport)
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "package %s\n\nimport (\n", pkg)
	for _, line := range imports {
		fmt.Fprintf(&builder, "\t%s\n", line)
	}
	builder.WriteString(")\n\n")
	builder.WriteString("// NewLogger builds this service's logger: one wide event per request. The drains\n")
	builder.WriteString("// and the identity come from the environment, so a deployment changes no code.\n")
	builder.WriteString("func NewLogger() *wlog.Logger {\n")
	builder.WriteString("\treturn wlog.New(\n\t\tsetup.FromEnv(),\n")
	fmt.Fprintf(&builder, "\t\twlog.WithService(%q, \"0.0.1\", \"local\"),\n", module)
	builder.WriteString("\t)\n}\n")
	if middlewareFunc != "" {
		builder.WriteString("\n")
		builder.WriteString(middlewareFunc)
		builder.WriteString("\n")
	}

	// go/format sorts the import block and aligns the code, so the file a user commits is
	// already gofmt-clean.
	formatted, err := format.Source([]byte(builder.String()))
	if err != nil {
		return setup{}, fmt.Errorf("format the generated setup: %w", err)
	}
	return setup{wlogGo: string(formatted), envExample: env}, nil
}

// drainFor returns the .env.example lines for one drain. The generated code names no drain,
// because setup.FromEnv builds the drains WLOG_DRAINS names.
func drainFor(drain string) string {
	switch drain {
	case "", "stdout":
		return "# No drain yet. Events go to stdout. Set WLOG_DRAINS to add one.\n"
	case "axiom":
		return "WLOG_DRAINS=axiom\nAXIOM_TOKEN=\nAXIOM_DATASET=\n"
	case "loki":
		return "WLOG_DRAINS=loki\nLOKI_URL=http://localhost:3100\n"
	case "file":
		return "WLOG_DRAINS=file\nWLOG_FILE_PATH=.logs/events.ndjson\n"
	default:
		return ""
	}
}
