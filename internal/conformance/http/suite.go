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
	"embed"
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

// Scenario is one request and the event it must produce. The golden event of a scenario
// is a hand-written file under testdata, named after the scenario.
type Scenario struct {
	Name     string
	Method   string
	Path     string
	Headers  map[string]string
	Body     string
	Handler  func(Routes) http.HandlerFunc
	Settings Settings
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
	want, err := golden(scenario.Name)
	if err != nil {
		t.Errorf("%s: %v", scenario.Name, err)
		return
	}
	got := conformance.Normalize(events[0])
	if diff := conformance.Diff(conformance.Normalize(want), got); diff != "" {
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

// goldens holds one hand-written golden event per scenario, named after the scenario.
// tools/cmd/schema validates every file against schema/event.v1.json.
//
//go:embed testdata/*.json
var goldens embed.FS

// golden returns the hand-written golden event of one scenario.
func golden(name string) (map[string]any, error) {
	body, err := goldens.ReadFile("testdata/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("load the golden of %s: %w", name, err)
	}
	event := map[string]any{}
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("parse the golden of %s: %w", name, err)
	}
	return event, nil
}

// scenarios is every HTTP scenario of the suite. The golden of each one is hand-written
// from the tables of SPEC-http-core.md and the summary rules of SPEC-core-v2.md, and it
// lives under testdata.
var scenarios = []Scenario{
	{
		Name: "OKFields", Method: http.MethodGet, Path: "/ok",
		Handler: func(r Routes) http.HandlerFunc { return r.OK },
	},
	{
		Name: "RouteTemplate", Method: http.MethodPost, Path: "/orders/42",
		Handler: func(r Routes) http.HandlerFunc { return r.Order },
	},
	{
		Name: "UnmatchedRoute", Method: http.MethodGet, Path: "/nope",
		Handler: func(r Routes) http.HandlerFunc { return r.OK },
	},
	{
		Name: "ServerErrorLevel", Method: http.MethodGet, Path: "/status/503",
		Handler: func(r Routes) http.HandlerFunc { return r.Status },
	},
	{
		Name: "ClientErrorLevel", Method: http.MethodGet, Path: "/status/429",
		Handler: func(r Routes) http.HandlerFunc { return r.Status },
	},
	{
		Name: "HandlerField", Method: http.MethodGet, Path: "/ok",
		Handler: func(r Routes) http.HandlerFunc {
			return func(w http.ResponseWriter, req *http.Request) {
				wlog.Set(req.Context(), "order_id", "A-1")
				w.WriteHeader(http.StatusOK)
			}
		},
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
		Secret:  "s3cret",
	},
	{
		Name: "UserAgent", Method: http.MethodGet, Path: "/ok",
		Headers: map[string]string{"User-Agent": "conformance/1.0"},
		Handler: func(r Routes) http.HandlerFunc { return r.OK },
	},
	{
		Name: "JSONBody", Method: http.MethodPost, Path: "/orders/42",
		Headers:  map[string]string{"Content-Type": "application/json"},
		Body:     `{"order_id":"A-1"}`,
		Settings: Settings{CaptureAll: true, MaxBody: 64},
		Handler:  func(r Routes) http.HandlerFunc { return r.Order },
	},
	{
		Name: "ArrayBody", Method: http.MethodPost, Path: "/orders/42",
		Headers:  map[string]string{"Content-Type": "application/json"},
		Body:     `[{"password":"hunter2"}]`,
		Settings: Settings{CaptureAll: true, MaxBody: 64},
		Handler:  func(r Routes) http.HandlerFunc { return r.Order },
		Secret:   "hunter2",
	},
	{
		Name: "CutBody", Method: http.MethodPost, Path: "/orders/42",
		Headers:  map[string]string{"Content-Type": "application/json"},
		Body:     `{"pad":"xxxxxxxxxx"}`,
		Settings: Settings{CaptureAll: true, MaxBody: 8},
		Handler:  func(r Routes) http.HandlerFunc { return r.Order },
	},
	{
		Name: "NoContent", Method: http.MethodGet, Path: "/status/204",
		Settings: Settings{CaptureAll: true, MaxBody: 64},
		Handler:  func(r Routes) http.HandlerFunc { return r.Status },
	},
	{
		Name: "NotModified", Method: http.MethodGet, Path: "/status/304",
		Settings: Settings{CaptureAll: true, MaxBody: 64},
		Handler:  func(r Routes) http.HandlerFunc { return r.Status },
	},
	{
		Name: "SpoofedForwardedFor", Method: http.MethodGet, Path: "/ok",
		Headers: map[string]string{"X-Forwarded-For": "10.0.0.1, 192.0.2.7"},
		Handler: func(r Routes) http.HandlerFunc { return r.OK },
	},
	{
		Name: "HeadCapturesNoBody", Method: http.MethodHead, Path: "/ok",
		Settings: Settings{CaptureAll: true, MaxBody: 64},
		Handler:  func(r Routes) http.HandlerFunc { return r.OK },
	},
	{
		Name: "PanicRecovered", Method: http.MethodGet, Path: "/panic",
	},
	{
		Name: "StreamFlushes", Method: http.MethodGet, Path: "/stream",
	},
	{
		Name: "ErrorAfterCommit", Method: http.MethodGet, Path: "/fail",
	},
}
