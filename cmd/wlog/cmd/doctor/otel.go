// This file holds the OTel ordering check of wlog doctor: the OTel middleware must wrap
// outside wlog, so the span exists before the event starts and the event carries its ids.
package doctor

import "strings"

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
	"otelgrpc.NewServerHandler",
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
	return otelOrderCheck(readSources(dir))
}

// otelOrderCheck reports the order of the two middleware calls in one source body. The
// call that starts first wraps the other one, so the first token names the outer layer.
func otelOrderCheck(sources string) Check {
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
