// This file holds the OTel ordering check of wlog doctor: the OTel middleware must wrap
// outside wlog, so the span exists before the event starts and the event carries its ids.
package doctor

import (
	"os"
	"path/filepath"
	"strings"
)

// otelMiddlewareTokens name the OTel middleware constructors. Each one must sit outside
// the wlog middleware in the wrapping expression.
var otelMiddlewareTokens = []string{
	"otelhttp.NewHandler",
	"otelhttp.NewMiddleware",
	"otelchi.Middleware",
	"otelgin.Middleware",
	"otelecho.Middleware",
	"otelfiber.Middleware",
	"otelgrpc.UnaryServerInterceptor",
	"otelgrpc.StreamServerInterceptor",
	"otelconnect.NewInterceptor",
}

// wlogMiddlewareTokens name the wlog middleware constructors.
var wlogMiddlewareTokens = []string{
	"wlogchi.Middleware", "wlogchi.Setup",
	"wlogfasthttp.Middleware",
	"wlogfiber.Middleware", "wlogfiber.Setup",
	"wlogfiber3.Middleware", "wlogfiber3.Setup",
	"wloghttprouter.New",
	"wloggozero.RouterOption",
	"wloghertz.Setup",
	"wlogkratos.Filter",
	"wloghuma.Middleware",
	"wlogstd.Middleware", "wlogstd.Setup",
	"wloggin.Middleware", "wloggin.Setup",
	"wlogecho.Middleware", "wlogecho.Setup",
	"wlogecho5.Middleware", "wlogecho5.Setup",
	"wloggrpc.UnaryServerInterceptor", "wloggrpc.ServerOptions",
	"wlogconnect.Interceptor",
	"wloggqlgen.Extension",
	"wlogtwirp.ServerHooks",
}

// checkOTelOrder warns when the OTel middleware sits inside the wlog middleware, because
// the span then starts after the event and the event carries no span id.
func checkOTelOrder(dir string) Check {
	return otelOrderFiles(readGoFiles(dir))
}

// otelOrderFiles warns when any one file puts wlog outside OTel. A correct file
// does not hide a wrong file, because each file is checked on its own.
func otelOrderFiles(files []string) Check {
	best := passCheck("otel", "WLOG_DOCTOR_OTEL_ORDER", "no OTel middleware is installed",
		"the OTel middleware must wrap outside wlog, so the event carries the span ids",
		"install the OTel middleware first when the app uses OTel")
	for _, file := range files {
		check := otelOrderCheck(file)
		if check.Status == "warn" {
			return check
		}
		if check.Message == "the OTel middleware wraps outside wlog" || check.Message == "no wlog middleware sits next to OTel" {
			best = check
		}
	}
	return best
}

// readGoFiles returns the Go sources in dir, one string per file.
func readGoFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, item.Name()))
		if err != nil {
			continue
		}
		files = append(files, string(source))
	}
	return files
}

// otelOrderCheck reports the order of the two middleware calls in one source body. The
// call that starts first wraps the other one, so the first token names the outer layer.
func otelOrderCheck(sources string) Check {
	sources = stripGoComments(sources)
	otelAt := firstToken(sources, otelMiddlewareTokens)
	if otelAt < 0 {
		return passCheck("otel", "WLOG_DOCTOR_OTEL_ORDER", "no OTel middleware is installed",
			"the OTel middleware must wrap outside wlog, so the event carries the span ids",
			"install the OTel middleware first when the app uses OTel")
	}
	wlogAt := firstToken(sources, wlogMiddlewareTokens)
	if wlogAt < 0 {
		return passCheck("otel", "WLOG_DOCTOR_OTEL_ORDER", "no wlog middleware sits next to OTel",
			"the OTel middleware must wrap outside wlog, so the event carries the span ids",
			"install the wlog middleware inside the OTel middleware")
	}
	if wlogAt < otelAt {
		return warnCheck("otel", "WLOG_DOCTOR_OTEL_ORDER", "the OTel middleware sits inside wlog",
			"an inner span starts after the event, so the event carries no span id",
			"install the OTel middleware first, so it wraps outside the wlog middleware")
	}
	return passCheck("otel", "WLOG_DOCTOR_OTEL_ORDER", "the OTel middleware wraps outside wlog",
		"the OTel middleware must wrap outside wlog, so the event carries the span ids",
		"install the OTel middleware first, so it wraps outside the wlog middleware")
}

// firstToken returns the position of the first token that appears in sources, or -1.
func firstToken(sources string, tokens []string) int {
	first := -1
	for _, token := range tokens {
		at := strings.Index(sources, token)
		if at < 0 {
			continue
		}
		if first < 0 || at < first {
			first = at
		}
	}
	return first
}

// stripGoComments removes line comments and block comments. Text inside a string
// or a rune stays, so a comment is not treated as code.
func stripGoComments(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			nl := strings.IndexByte(src[i:], '\n')
			if nl < 0 {
				return b.String()
			}
			i += nl
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 4
		case src[i] == '"' || src[i] == '`' || src[i] == '\'':
			end := scanQuote(src, i)
			b.WriteString(src[i:end])
			i = end
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

// scanQuote returns the index just after the string or rune that starts at i.
func scanQuote(src string, i int) int {
	quote := src[i]
	i++
	if quote == '`' {
		if end := strings.IndexByte(src[i:], '`'); end >= 0 {
			return i + end + 1
		}
		return len(src)
	}
	for i < len(src) {
		if src[i] == '\\' && i+1 < len(src) {
			i += 2
			continue
		}
		if src[i] == quote {
			return i + 1
		}
		i++
	}
	return len(src)
}
