// This file tests the capture policy of the HTTP core black box: what safe defaults keep,
// what CaptureAll adds, and how a route or a path rule changes the answer.
package httpcore_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestHTTPCore_HTTP13_SpoofedForwardedFor proves that a forwarded address counts only
// from a trusted proxy, and that a huge request id is replaced.
func TestHTTPCore_HTTP13_SpoofedForwardedFor(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.0.2.7")
	req.Header.Set("X-Request-ID", strings.Repeat("a", 8*1024))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	fields := httpFields(t, rec.Last())
	if fields["client_ip"] != "203.0.113.9" {
		t.Errorf("client_ip = %v, want the connection address", fields["client_ip"])
	}
	trace, _ := rec.Last()["trace"].(map[string]any)
	id, _ := trace["request_id"].(string)
	if id == "" || len(id) > 128 {
		t.Errorf("request_id = %q, want a generated id of 128 characters or fewer", id)
	}
	if body := eventJSON(t, rec.Last()); strings.Contains(body, "192.0.2.7") {
		t.Errorf("the spoofed address reached the event: %s", body)
	}
}

// TestHTTPCore_HTTP14_SessionValuesNeverKept proves that no session, key, or signature
// value reaches an event under safe defaults.
func TestHTTPCore_HTTP14_SessionValuesNeverKept(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/orders?code=c-1&key=k-1&sig=s-1", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: "sid-value"})
	req.AddCookie(&http.Cookie{Name: "PHPSESSID", Value: "sess-value"})
	handler.ServeHTTP(httptest.NewRecorder(), req)

	body := eventJSON(t, rec.Last())
	for _, secret := range []string{"c-1", "k-1", "s-1", "sid-value", "sess-value"} {
		if strings.Contains(body, secret) {
			t.Errorf("the value %q reached the event: %s", secret, body)
		}
	}
}

// TestHTTPCore_HTTP17_ForRouteAndGlobs proves that a skipped path starts no event, that a
// ** glob crosses a slash, and that a ForRoute rule changes a matching route only.
func TestHTTPCore_HTTP17_ForRouteAndGlobs(t *testing.T) {
	skipped, skipRec := wlogtest.New(t)
	skipper := httpcore.NetHTTP(skipped, httpcore.SkipPaths("/health", "/metrics/**"))(okHandler())
	for _, path := range []string{"/health", "/metrics/latency/p95"} {
		skipper.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if skipRec.Count() != 0 {
			t.Errorf("the skipped path %s started an event", path)
		}
	}
	skipper.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders", nil))
	if skipRec.Count() != 1 {
		t.Errorf("events = %d after one kept path, want 1", skipRec.Count())
	}

	logged, rec := wlogtest.New(t)
	header := httpcore.NetHTTP(logged,
		httpcore.RouteFunc(func(r *http.Request) string { return r.URL.Path }),
		httpcore.ForRoute("GET /orders/*", httpcore.CaptureResponseHeaders("x-tenant")),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Tenant", "t-1")
		w.WriteHeader(http.StatusOK)
	}))

	header.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/42", nil))
	if got := responseHeaders(t, rec.Last())["x-tenant"]; got != "t-1" {
		t.Errorf("a matching route kept x-tenant = %v, want t-1", got)
	}

	header.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/carts/42", nil))
	if _, kept := responseHeaders(t, rec.Last())["x-tenant"]; kept {
		t.Errorf("a route the rule does not match kept x-tenant: %v", responseHeaders(t, rec.Last()))
	}
}

// TestHTTPCore_PAR15_CaptureAll proves that CaptureAll captures values, that an unlisted
// cookie value is masked, and that CookieValues keeps the one it names.
func TestHTTPCore_PAR15_CaptureAll(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithService("shop", "1.0.0", "prod"))
	handler := httpcore.NetHTTP(log, httpcore.CaptureAll(), httpcore.CookieValues("theme"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Tenant", "t-1")
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest(http.MethodGet, "/orders?page=2", nil)
	req.Header.Set("X-Tenant", "t-1")
	req.AddCookie(&http.Cookie{Name: "theme", Value: "dark"})
	req.AddCookie(&http.Cookie{Name: "sid", Value: "abc"})
	handler.ServeHTTP(httptest.NewRecorder(), req)

	fields := httpFields(t, rec.Last())
	query, _ := fields["request_query"].(map[string]any)
	if query["page"] != "2" {
		t.Errorf("request_query = %v, want the value under CaptureAll", query)
	}
	if _, kept := requestHeaders(t, rec.Last())["x-tenant"]; !kept {
		t.Errorf("CaptureAll kept no x-tenant: %v", requestHeaders(t, rec.Last()))
	}
	cookies, _ := fields["request_cookies"].(map[string]any)
	if cookies["theme"] != "dark" {
		t.Errorf("cookies = %v, want the listed theme kept", cookies)
	}
	if cookies["sid"] == "abc" {
		t.Errorf("the unlisted cookie value was kept: %v", cookies)
	}
	if _, kept := responseHeaders(t, rec.Last())["x-tenant"]; !kept {
		t.Errorf("CaptureAll kept no response x-tenant: %v", responseHeaders(t, rec.Last()))
	}
}

// TestHTTPCore_PAR15_EnvAndSkip proves that a development service environment captures
// all, that Skip starts no event, and that the request id rules follow their options.
func TestHTTPCore_PAR15_EnvAndSkip(t *testing.T) {
	local, localRec := wlogtest.New(t, wlog.WithService("shop", "1.0.0", "local"))
	httpcore.NetHTTP(local)(okHandler()).ServeHTTP(
		httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders?page=2", nil))
	if query, _ := httpFields(t, localRec.Last())["request_query"].(map[string]any); query["page"] != "2" {
		t.Errorf("a local environment kept no query value: %v", localRec.Last())
	}

	log, rec := wlogtest.New(t, wlog.WithService("shop", "1.0.0", "prod"))
	handler := httpcore.NetHTTP(log,
		httpcore.Skip(func(r httpcore.Request) bool { return r.Header("X-Skip") == "1" }),
		httpcore.TrustRequestID(false),
		httpcore.EchoRequestID(false),
	)(okHandler())

	skipReq := httptest.NewRequest(http.MethodGet, "/orders", nil)
	skipReq.Header.Set("X-Skip", "1")
	handler.ServeHTTP(httptest.NewRecorder(), skipReq)
	if rec.Count() != 0 {
		t.Errorf("a skipped request started an event")
	}

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	req.Header.Set("X-Request-ID", "client-chosen")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	trace, _ := rec.Last()["trace"].(map[string]any)
	if trace["request_id"] == "client-chosen" {
		t.Errorf("an untrusted request id was kept: %v", trace["request_id"])
	}
	if echoed := w.Header().Get("X-Request-ID"); echoed != "" {
		t.Errorf("EchoRequestID(false) wrote %q", echoed)
	}
}

// TestHTTPCore_PAR15_TrustedProxyAndExtras proves that a trusted proxy supplies the
// client address, that CaptureHeaders adds a name, and that User sets user.id.
func TestHTTPCore_PAR15_TrustedProxyAndExtras(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log,
		httpcore.TrustedProxies("10.0.0.0/8"),
		httpcore.CaptureHeaders("X-Tenant"),
		httpcore.User(func(r httpcore.Request) string { return r.Header("X-User") }),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.5")
	req.Header.Set("X-Tenant", "t-1")
	req.Header.Set("X-User", "u-1")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	fields := httpFields(t, rec.Last())
	if fields["client_ip"] != "203.0.113.7" {
		t.Errorf("client_ip = %v, want the first untrusted forwarded address", fields["client_ip"])
	}
	if fields["bytes_out"] != int64(2) {
		t.Errorf("bytes_out = %v, want the two bytes the handler wrote", fields["bytes_out"])
	}
	if got := requestHeaders(t, rec.Last())["x-tenant"]; got != "t-1" {
		t.Errorf("request_headers x-tenant = %v, want the added name", got)
	}
	user, _ := rec.Last()["user"].(map[string]any)
	if user["id"] != "u-1" {
		t.Errorf("user.id = %v, want u-1", user["id"])
	}
}

// requestHeaders returns the request header group of an event.
func requestHeaders(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	headers, _ := httpFields(t, event)["request_headers"].(map[string]any)
	return headers
}

// okHandler answers every request with a 200 and no body.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// responseHeaders returns the response header group of an event.
func responseHeaders(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	headers, _ := httpFields(t, event)["response_headers"].(map[string]any)
	return headers
}

// eventJSON renders one event as text, so a test can prove that a value never appears.
func eventJSON(t *testing.T, event map[string]any) string {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(body)
}

// TestHTTPCore_PAR15_SafeDefaults proves that safe defaults keep the allow-listed headers
// and the names of the query keys and the cookies, and nothing else.
func TestHTTPCore_PAR15_SafeDefaults(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Tenant", "t-1")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/orders?page=2&limit=10", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "abc"})
	handler.ServeHTTP(httptest.NewRecorder(), req)

	fields := httpFields(t, rec.Last())
	requestHeaders, _ := fields["request_headers"].(map[string]any)
	if requestHeaders["accept"] != "application/json" {
		t.Errorf("request_headers = %v, want the allow-listed accept", requestHeaders)
	}
	if _, kept := requestHeaders["authorization"]; kept {
		t.Errorf("the default allow-list kept authorization: %v", requestHeaders)
	}
	if got := fields["request_query_keys"]; !reflect.DeepEqual(got, []any{"limit", "page"}) {
		t.Errorf("request_query_keys = %v, want the sorted names", got)
	}
	// The redactor masks the cookie-name field, because its key holds the word cookie.
	// The field is still captured, and the value never reaches the event.
	if _, kept := fields["request_cookie_names"]; !kept {
		t.Errorf("request_cookie_names is absent: %v", fields)
	}
	if strings.Contains(eventJSON(t, rec.Last()), "abc") {
		t.Errorf("the cookie value reached the event: %v", rec.Last())
	}
	if _, kept := fields["request_query"]; kept {
		t.Errorf("the default policy kept query values: %v", fields["request_query"])
	}
	if _, kept := fields["request_cookies"]; kept {
		t.Errorf("the default policy kept cookie values: %v", fields["request_cookies"])
	}

	responseHeaders, _ := fields["response_headers"].(map[string]any)
	if responseHeaders["content-type"] != "application/json" {
		t.Errorf("response_headers = %v, want the allow-listed content-type", responseHeaders)
	}
	if _, kept := responseHeaders["x-tenant"]; kept {
		t.Errorf("the default allow-list kept x-tenant: %v", responseHeaders)
	}
}
