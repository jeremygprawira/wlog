// This file tests the framework-neutral HTTP core black box: the operation name, the
// level, the route rules, and the scheme and host fields.
package httpcore_test

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestHTTPCore_CORE14_OperationIsRoute proves that the operation names the route template
// of a matched route, and reports unmatched when the router matched none.
//
// A Go 1.22 or later router sets the pattern on the request it dispatches, which is what
// net/http's own ServeMux does. The handler sets it here, so the test does not depend on
// the mux behavior of the toolchain that runs it. The field arrived in Go 1.23, so a test
// on an older toolchain skips the matched case.
func TestHTTPCore_CORE14_OperationIsRoute(t *testing.T) {
	log, rec := wlogtest.New(t)
	if patternField(httptest.NewRequest(http.MethodGet, "/", nil)).Kind() != reflect.String {
		t.Skip("this toolchain predates the Pattern field of net/http")
	}
	handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		patternField(r).SetString("GET /orders/{id}")
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/42", nil))
	if got := rec.Last()["operation"]; got != "GET /orders/{id}" {
		t.Errorf("operation = %v, want the route template", got)
	}
	if got := httpFields(t, rec.Last())["route"]; got != "/orders/{id}" {
		t.Errorf("http.route = %v, want the route template", got)
	}

	unmatched := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	unmatched.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))
	if got := rec.Last()["operation"]; got != "GET unmatched" {
		t.Errorf("operation = %v, want GET unmatched", got)
	}
}

// patternField returns the Pattern field of a request by name. A toolchain older than Go
// 1.23 has no such field, so the value is invalid there and the caller can skip.
func patternField(r *http.Request) reflect.Value {
	return reflect.ValueOf(r).Elem().FieldByName("Pattern")
}

// TestHTTPCore_CORE15_LevelFromStatus proves that the level follows the status, and that
// a recorded 4xx error gives warn while a 5xx error gives error.
func TestHTTPCore_CORE15_LevelFromStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		level  string
	}{
		{"200", http.StatusOK, "info"},
		{"404", http.StatusNotFound, "warn"},
		{"500", http.StatusInternalServerError, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t)
			handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
			if got := rec.Last()["level"]; got != tc.level {
				t.Errorf("level = %v, want %s", got, tc.level)
			}
		})
	}

	for _, tc := range []struct {
		name        string
		errorStatus int
		status      int
		level       string
	}{
		{"400 error", http.StatusBadRequest, http.StatusBadRequest, "warn"},
		{"503 error", http.StatusServiceUnavailable, http.StatusServiceUnavailable, "error"},
		{"400 error, 500 response", http.StatusBadRequest, http.StatusInternalServerError, "warn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t, wlog.WithErrorExtractor(statusExtractor{}))
			handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wlog.Error(r.Context(), statusError{status: tc.errorStatus})
				w.WriteHeader(tc.status)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
			if got := rec.Last()["level"]; got != tc.level {
				t.Errorf("level = %v, want %s", got, tc.level)
			}
		})
	}
}

// TestHTTPCore_HTTP15_UnmatchedRoute proves that a wildcard template gives an empty route
// for a 404 or a 405, and keeps it for a status that matched.
func TestHTTPCore_HTTP15_UnmatchedRoute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		route  string
	}{
		{"wildcard 404", http.StatusNotFound, ""},
		{"wildcard 405", http.StatusMethodNotAllowed, ""},
		{"wildcard 200", http.StatusOK, "/*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t)
			wildcard := func(*http.Request) string { return "/*" }
			handler := httpcore.NetHTTP(log, httpcore.RouteFunc(wildcard))(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.status)
				}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))
			if got := httpFields(t, rec.Last())["route"]; got != tc.route {
				t.Errorf("http.route = %v, want %q", got, tc.route)
			}
		})
	}
}

// TestHTTPCore_SchemeHost proves that a TLS connection is https, that the Host header
// names the host, and that a forwarded header counts only from a trusted proxy.
func TestHTTPCore_SchemeHost(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tls        bool
		remoteAddr string
		forwarded  bool
		scheme     string
		host       string
	}{
		{"plain", false, "203.0.113.9:1234", false, "http", "shop.example"},
		{"tls", true, "203.0.113.9:1234", false, "https", "shop.example"},
		{"trusted proxy", false, "10.0.0.5:1234", true, "https", "edge.example"},
		{"untrusted client", false, "203.0.113.9:1234", true, "http", "shop.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t)
			handler := httpcore.NetHTTP(log, httpcore.TrustedProxies("10.0.0.0/8"))(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))

			req := httptest.NewRequest(http.MethodGet, "http://shop.example/orders", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tc.forwarded {
				req.Header.Set("X-Forwarded-Proto", "https")
				req.Header.Set("X-Forwarded-Host", "edge.example")
			}

			handler.ServeHTTP(httptest.NewRecorder(), req)
			fields := httpFields(t, rec.Last())
			if fields["scheme"] != tc.scheme {
				t.Errorf("http.scheme = %v, want %q", fields["scheme"], tc.scheme)
			}
			if fields["host"] != tc.host {
				t.Errorf("http.host = %v, want %q", fields["host"], tc.host)
			}
		})
	}
}

// statusError carries the HTTP status that decided it.
type statusError struct{ status int }

// Error returns the message of the failure.
func (e statusError) Error() string { return "declined" }

// statusExtractor reports the status an error carries.
type statusExtractor struct{}

// Extract returns an error detail with the status of the error.
func (statusExtractor) Extract(err error) wlog.ErrorInfo {
	var withStatus statusError
	if errors.As(err, &withStatus) {
		return wlog.ErrorInfo{Message: err.Error(), Status: withStatus.status}
	}
	return wlog.ErrorInfo{Message: err.Error()}
}

// httpFields returns the http group of an event.
func httpFields(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	fields, ok := event["http"].(map[string]any)
	if !ok {
		t.Fatalf("the event carries no http group: %v", event)
	}
	return fields
}
