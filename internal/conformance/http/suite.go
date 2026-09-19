// Package httpconformance is the HTTP conformance suite: one fixed route table, one
// scenario per behavior, and three checks per scenario. Every HTTP adapter in this repo
// runs it, so "agnostic" is a test result rather than a claim.
//
// The suite owns the routes, so no adapter has to agree with another about path-parameter
// syntax. An adapter gives a Factory, and the suite drives the handler it builds.
//
// Each scenario checks three things. The normalized event equals the golden event of the
// scenario, no secret of the scenario appears anywhere in the recorded output, and the
// golden is valid against schema/event.v1.json. The schema check runs in tools/cmd/schema,
// because the root module holds no JSON Schema library.
package httpconformance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/memory"
	"github.com/jeremygprawira/wlog/internal/conformance"
)

// requestID is the request id every scenario sends, so the event holds a stable value.
const requestID = "conformance-1"

// Routes is the fixed route table every adapter serves.
type Routes struct {
	OK     http.HandlerFunc // GET /ok
	Order  http.HandlerFunc // POST /orders/{id}
	Panic  http.HandlerFunc // GET /panic
	Status http.HandlerFunc // GET /status/{code}
	Stream http.HandlerFunc // GET /stream
	Fail   http.HandlerFunc // GET /fail
}

// Settings is what one scenario asks an adapter to change. The suite names the behavior,
// and the adapter maps it to its own option.
type Settings struct {
	CaptureAll bool
	MaxBody    int
}

// Factory builds one adapter around the route table. The adapter applies its own skip rule
// to /skip, and its own route function, so the suite sees the template the router gave it.
type Factory interface {
	Build(log *wlog.Logger, routes Routes, settings Settings) http.Handler
}

// TB is the part of testing.T the suite uses, so a test can run the suite against a broken
// adapter and read the failures it reports.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Run(name string, fn func(TB)) bool
}

// Scenario is one request and the event it must produce. Golden holds the normalized
// event, hand-written from the tables of SPEC-http-core.md.
type Scenario struct {
	Name     string
	Method   string
	Path     string
	Headers  map[string]string
	Body     string
	Handler  func(Routes) http.HandlerFunc
	Settings Settings
	Golden   map[string]any
	Secret   string // a value that must never appear in the recorded output
	NoEvents bool   // true for a scenario that starts no event at all
}

// Run runs every scenario against the adapter the factory builds.
func Run(t TB, factory Factory) {
	t.Helper()
	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.Name, func(t TB) {
			t.Helper()
			runScenario(t, factory, scenario)
		})
	}
}

// runScenario serves one request and checks the event it produced.
func runScenario(t TB, factory Factory, scenario Scenario) {
	t.Helper()
	rec := newRecorder()
	handler := factory.Build(rec.logger(), routesFor(scenario), scenario.Settings)

	// The request is built here, not by httptest.NewRequest, because that helper carries
	// no context and the module keeps a Go 1.21 floor.
	req, err := http.NewRequestWithContext(context.Background(), scenario.Method, scenario.Path, strings.NewReader(scenario.Body))
	if err != nil {
		t.Errorf("%s: build the request: %v", scenario.Name, err)
		return
	}
	req.Host = "example.com"
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Request-ID", requestID)
	for name, value := range scenario.Headers {
		req.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if scenario.NoEvents {
		if count := len(rec.events()); count != 0 {
			t.Errorf("%s: events = %d, want none", scenario.Name, count)
		}
		return
	}

	events := rec.events()
	if len(events) != 1 {
		t.Errorf("%s: events = %d, want 1", scenario.Name, len(events))
		return
	}
	got := conformance.Normalize(events[0])
	if diff := conformance.Diff(scenario.Golden, got); diff != "" {
		t.Errorf("%s: the event differs from the golden:\n%s", scenario.Name, diff)
	}
	if scenario.Secret != "" {
		if body, err := json.Marshal(events[0]); err == nil && strings.Contains(string(body), scenario.Secret) {
			t.Errorf("%s: the secret %q reached the event", scenario.Name, scenario.Secret)
		}
	}
}

// recorder collects the events and the problems of one run.
type recorder struct {
	mem      *memory.Memory
	mu       sync.Mutex
	problems []wlog.Problem
}

// newRecorder builds an empty recorder.
func newRecorder() *recorder {
	return &recorder{mem: memory.New(0)}
}

// logger builds the Logger of one scenario. It is silent, so a suite run prints nothing,
// and it reports a problem into the recorder rather than to stderr.
func (r *recorder) logger(opts ...wlog.Option) *wlog.Logger {
	all := append([]wlog.Option{
		wlog.WithSilent(),
		wlog.WithDrains(r.mem),
		wlog.WithRedactFingerprint(false),
		wlog.OnProblem(func(p wlog.Problem) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.problems = append(r.problems, p)
		}),
	}, opts...)
	return wlog.New(all...)
}

// events returns every event the run recorded.
func (r *recorder) events() []map[string]any { return r.mem.Snapshot() }

// routesFor returns the route table of one scenario, with the handler of the scenario in
// the place of the route it drives.
func routesFor(scenario Scenario) Routes {
	table := routes()
	if scenario.Handler == nil {
		return table
	}
	handler := scenario.Handler(table)
	switch {
	case strings.HasPrefix(scenario.Path, "/orders/"):
		table.Order = handler
	case strings.HasPrefix(scenario.Path, "/status/"):
		table.Status = handler
	case scenario.Path == "/panic":
		table.Panic = handler
	case scenario.Path == "/stream":
		table.Stream = handler
	case scenario.Path == "/fail":
		table.Fail = handler
	default:
		table.OK = handler
	}
	return table
}

// routes is the handler table every scenario shares.
func routes() Routes {
	return Routes{
		OK: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
		Order: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		},
		Panic: func(http.ResponseWriter, *http.Request) {
			panic("conformance panic")
		},
		Status: func(w http.ResponseWriter, r *http.Request) {
			code := 500
			if _, err := fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/status/"), "%d", &code); err != nil {
				code = 500
			}
			w.WriteHeader(code)
		},
		Stream: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "chunk")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		},
		Fail: func(w http.ResponseWriter, r *http.Request) {
			wlog.Error(r.Context(), fmt.Errorf("conformance failure"))
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}

// httpFields returns the http group of one golden event.
func httpFields(golden map[string]any) map[string]any {
	fields, _ := golden["http"].(map[string]any)
	return fields
}

// base is the event a matched route starts from, before a scenario adds its own fields.
// The caller passes the level and the outcome the scenario expects.
func base(method, operation, route, path string, status int, level, outcome string) map[string]any {
	return map[string]any{
		"level": level, "kind": "request", "outcome": outcome, "operation": operation,
		"summary": fmt.Sprintf("%s %s %d in {d}", method, routeName(route), status),
		"http": map[string]any{
			"method": method, "route": route, "path": path,
			"protocol": "HTTP/1.1", "scheme": "http", "host": "example.com", "status": status,
			"bytes_in": 0, "bytes_out": 0, "client_ip": "192.0.2.1", "user_agent": "",
		},
		"trace": map[string]any{"request_id": requestID},
		"wlog":  map[string]any{"schema_version": 2},
	}
}

// routeName returns the route text a summary carries, which is unmatched for an empty
// route.
func routeName(route string) string {
	if route == "" {
		return "unmatched"
	}
	return route
}

// scenarios is every HTTP scenario of the suite. The golden of each one is hand-written
// from the tables of SPEC-http-core.md and the summary rules of SPEC-core-v2.md.
var scenarios = []Scenario{
	{
		Name: "OKFields", Method: http.MethodGet, Path: "/ok",
		Handler: func(r Routes) http.HandlerFunc { return r.OK },
		Golden:  base("GET", "GET /ok", "/ok", "/ok", http.StatusOK, "info", "success"),
	},
	{
		Name: "RouteTemplate", Method: http.MethodPost, Path: "/orders/42",
		Handler: func(r Routes) http.HandlerFunc { return r.Order },
		Golden:  base("POST", "POST /orders/{id}", "/orders/{id}", "/orders/42", http.StatusCreated, "info", "success"),
	},
	{
		Name: "UnmatchedRoute", Method: http.MethodGet, Path: "/nope",
		Handler: func(r Routes) http.HandlerFunc { return r.OK },
		Golden: func() map[string]any {
			golden := base("GET", "GET unmatched", "", "/nope", http.StatusNotFound, "warn", "success")
			// The mux answers an unmatched path itself, and its 404 carries a body.
			httpFields(golden)["bytes_out"] = 19
			httpFields(golden)["response_headers"] = map[string]any{"content-type": "text/plain; charset=utf-8"}
			return golden
		}(),
	},
	{
		Name: "ServerErrorLevel", Method: http.MethodGet, Path: "/status/503",
		Handler: func(r Routes) http.HandlerFunc { return r.Status },
		Golden:  base("GET", "GET /status/{code}", "/status/{code}", "/status/503", http.StatusServiceUnavailable, "error", "error"),
	},
	{
		Name: "ClientErrorLevel", Method: http.MethodGet, Path: "/status/429",
		Handler: func(r Routes) http.HandlerFunc { return r.Status },
		Golden:  base("GET", "GET /status/{code}", "/status/{code}", "/status/429", http.StatusTooManyRequests, "warn", "success"),
	},
	{
		Name: "HandlerField", Method: http.MethodGet, Path: "/ok",
		Handler: func(r Routes) http.HandlerFunc {
			return func(w http.ResponseWriter, req *http.Request) {
				wlog.Set(req.Context(), "order_id", "A-1")
				w.WriteHeader(http.StatusOK)
			}
		},
		Golden: func() map[string]any {
			golden := base("GET", "GET /ok", "/ok", "/ok", http.StatusOK, "info", "success")
			// The summary names up to two user keys that end in _id.
			golden["order_id"] = "A-1"
			golden["summary"] = "GET /ok 200 in {d} (order_id=A-1)"
			return golden
		}(),
	},
	{
		Name: "SkippedPath", Method: http.MethodGet, Path: "/skip",
		Handler:  func(r Routes) http.HandlerFunc { return r.OK },
		NoEvents: true,
	},
	{
		Name: "SafeDefaults", Method: http.MethodGet, Path: "/ok?page=2",
		Headers: map[string]string{"Accept": "application/json", "Authorization": "Bearer s3cret", "Cookie": "sid=abc"},
		Handler: func(r Routes) http.HandlerFunc { return r.OK },
		Golden: func() map[string]any {
			golden := base("GET", "GET /ok", "/ok", "/ok", http.StatusOK, "info", "success")
			fields := httpFields(golden)
			fields["request_headers"] = map[string]any{"accept": "application/json"}
			fields["request_query_keys"] = []any{"page"}
			// The redactor masks the cookie-name field, because its key holds the word
			// cookie. The names are still captured, and the values never are.
			fields["request_cookie_names"] = "[REDACTED]"
			return golden
		}(),
		Secret: "s3cret",
	},
	{
		Name: "UserAgent", Method: http.MethodGet, Path: "/ok",
		Headers: map[string]string{"User-Agent": "conformance/1.0"},
		Handler: func(r Routes) http.HandlerFunc { return r.OK },
		Golden: func() map[string]any {
			golden := base("GET", "GET /ok", "/ok", "/ok", http.StatusOK, "info", "success")
			httpFields(golden)["user_agent"] = "conformance/1.0"
			return golden
		}(),
	},
	{
		Name: "HeadCapturesNoBody", Method: http.MethodHead, Path: "/ok",
		Settings: Settings{CaptureAll: true, MaxBody: 64},
		Handler:  func(r Routes) http.HandlerFunc { return r.OK },
		Golden: func() map[string]any {
			golden := base("HEAD", "HEAD /ok", "/ok", "/ok", http.StatusOK, "info", "success")
			// CaptureAll keeps every header, and the middleware echoes the request id.
			fields := httpFields(golden)
			fields["request_headers"] = map[string]any{"x-request-id": requestID}
			fields["response_headers"] = map[string]any{"x-request-id": requestID}
			return golden
		}(),
	},
}
