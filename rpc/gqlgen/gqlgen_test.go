// This file drives the gqlgen extension through a real gqlgen server and a hand-written
// executable schema, so the tests need no generated code. It checks the operation fields,
// the complexity, the secret-free event, the fixed message of a bad body, and the recover
// wrapper.
package wloggqlgen_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
	wloggqlgen "github.com/jeremygprawira/wlog/rpc/gqlgen"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// testSchema is the smallest schema the tests need.
var testSchema = gqlparser.MustLoadSchema(&ast.Source{
	Name:  "schema.graphql",
	Input: "type Query { name: String! }\ntype Mutation { login(password: String!): String! }\n",
})

// fakeSchema is the executable schema the tests drive. It needs no generated code.
type fakeSchema struct {
	exec func(ctx context.Context) graphql.ResponseHandler
}

// Schema returns the parsed schema.
func (fakeSchema) Schema() *ast.Schema { return testSchema }

// Complexity answers one for every field.
func (fakeSchema) Complexity(string, string, int, map[string]any) (int, bool) { return 1, true }

// Exec returns the response handler of the test.
func (f fakeSchema) Exec(ctx context.Context) graphql.ResponseHandler { return f.exec(ctx) }

// TestGqlgen_C5_OperationFields proves that one operation records its name, its type, the
// complexity, and the query hash, and that a clean response carries no error.
func TestGqlgen_C5_OperationFields(t *testing.T) {
	log, rec := wlogtest.New(t)
	srv := newServer(t, log, okSchema)

	post(t, srv, `{"query":"query Name { name }"}`)

	got := rec.Last()
	if got["level"] != "info" || got["outcome"] != "success" {
		t.Errorf("level/outcome = %v/%v, want info/success", got["level"], got["outcome"])
	}
	fields, _ := got["rpc"].(map[string]any)
	if fields["system"] != "graphql" {
		t.Errorf("rpc.system = %v, want graphql", fields["system"])
	}
	if fields["method"] != "Name" {
		t.Errorf("rpc.method = %v, want Name", fields["method"])
	}
	graphqlFields, _ := fields["graphql"].(map[string]any)
	if graphqlFields == nil {
		t.Fatalf("rpc.graphql = %v, want the group", fields["graphql"])
	}
	if graphqlFields["type"] != "query" {
		t.Errorf("rpc.graphql.type = %v, want query", graphqlFields["type"])
	}
	if hash, _ := graphqlFields["query_sha256"].(string); len(hash) != 64 {
		t.Errorf("rpc.graphql.query_sha256 = %v, want a SHA-256", graphqlFields["query_sha256"])
	}
	if !conformance.Equal(graphqlFields["errors_count"], 0) {
		t.Errorf("rpc.graphql.errors_count = %v, want 0", graphqlFields["errors_count"])
	}
}

// TestGqlgen_C5_Complexity proves that the complexity stats of the limit extension reach
// the event.
func TestGqlgen_C5_Complexity(t *testing.T) {
	log, rec := wlogtest.New(t)
	srv := newServer(t, log, okSchema, func(s *handler.Server) {
		s.Use(extension.FixedComplexityLimit(100))
	})

	post(t, srv, `{"query":"query Name { name }"}`)

	fields, _ := rec.Last()["rpc"].(map[string]any)
	graphqlFields, _ := fields["graphql"].(map[string]any)
	if !conformance.Equal(graphqlFields["complexity"], 1) {
		t.Errorf("rpc.graphql.complexity = %v, want 1", graphqlFields["complexity"])
	}
	if !conformance.Equal(graphqlFields["complexity_limit"], 100) {
		t.Errorf("rpc.graphql.complexity_limit = %v, want 100", graphqlFields["complexity_limit"])
	}
}

// TestGqlgen_C5_PasswordNeverRecorded proves that a password in the variables and in an
// inline literal never reaches the event.
func TestGqlgen_C5_PasswordNeverRecorded(t *testing.T) {
	const secret = "hunter2"
	log, rec := wlogtest.New(t)
	srv := newServer(t, log, okSchema)

	post(t, srv, `{"query":"mutation M { login(password: \""`+secret+`"\") }","variables":{"password":"`+secret+`"}}`)

	body, err := json.Marshal(rec.Last())
	if err != nil {
		t.Fatalf("marshal the event: %v", err)
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("the secret %q reached the event: %s", secret, body)
	}
}

// TestGqlgen_C5_BadBodyFixedMessage proves that a body which fails to decode records a
// fixed message only, because the decode error holds the whole body.
func TestGqlgen_C5_BadBodyFixedMessage(t *testing.T) {
	const secret = "hunter2"
	log, rec := wlogtest.New(t)
	srv := newServer(t, log, okSchema)

	post(t, srv, `{"query":"{ name }","variables":{"password":"`+secret+`"}`)

	body, err := json.Marshal(rec.Last())
	if err != nil {
		t.Fatalf("marshal the event: %v", err)
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("the secret %q reached the event: %s", secret, body)
	}
	if rec.Last()["level"] != "warn" {
		t.Errorf("level = %v, want warn", rec.Last()["level"])
	}
	info, _ := rec.Last()["error"].(map[string]any)
	if info == nil {
		t.Fatalf("error = %v, want the fixed message", rec.Last()["error"])
	}
	if message, _ := info["message"].(string); strings.Contains(message, "password") {
		t.Errorf("error.message = %q, want the fixed message", message)
	}
}

// TestGqlgen_C5_ResolverError proves that a resolver error is recorded through the error
// extractor and gives level error.
func TestGqlgen_C5_ResolverError(t *testing.T) {
	log, rec := wlogtest.New(t)
	srv := newServer(t, log, func(ctx context.Context) graphql.ResponseHandler {
		return func(context.Context) *graphql.Response {
			return &graphql.Response{
				Data:   []byte(`{"name":null}`),
				Errors: gqlError("the resolver failed", errors.New("the resolver failed")),
			}
		}
	})

	post(t, srv, `{"query":"query Name { name }"}`)

	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
	info, _ := rec.Last()["error"].(map[string]any)
	if info["message"] != "the resolver failed" {
		t.Errorf("error.message = %v, want the resolver error", info["message"])
	}
	fields, _ := rec.Last()["rpc"].(map[string]any)
	graphqlFields, _ := fields["graphql"].(map[string]any)
	if !conformance.Equal(graphqlFields["errors_count"], 1) {
		t.Errorf("rpc.graphql.errors_count = %v, want 1", graphqlFields["errors_count"])
	}
	if graphqlFields["partial"] != true {
		t.Errorf("rpc.graphql.partial = %v, want true", graphqlFields["partial"])
	}
}

// TestGqlgen_C5_RecoverWraps proves that the recover wrapper records the panic with a
// stack, and that the user's recover function still answers.
func TestGqlgen_C5_RecoverWraps(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	called := false
	wrapped := wloggqlgen.Recover(func(context.Context, any) error {
		called = true
		return errors.New("the user message")
	})

	if err := wrapped(ctx, "boom"); err == nil || err.Error() != "the user message" {
		t.Errorf("Recover returned %v, want the user message", err)
	}
	end()

	if !called {
		t.Error("the user recover function never ran")
	}
	info, _ := rec.Last()["error"].(map[string]any)
	if info == nil || info["stack"] == "" {
		t.Errorf("error = %v, want a stack", rec.Last()["error"])
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
}

// okSchema answers every operation with one field.
func okSchema(context.Context) graphql.ResponseHandler {
	return func(context.Context) *graphql.Response {
		return &graphql.Response{Data: []byte(`{"name":"ok"}`)}
	}
}

// gqlError builds one error list entry with its wrapped error.
func gqlError(message string, err error) gqlerror.List {
	return gqlerror.List{{Message: message, Err: err}}
}

// newServer builds one gqlgen server with the extension, wrapped in the net/http
// middleware, so the extension adds to an open request event.
func newServer(t *testing.T, log *wlog.Logger, exec func(context.Context) graphql.ResponseHandler, setup ...func(*handler.Server)) http.Handler {
	t.Helper()
	srv := handler.New(fakeSchema{exec: exec})
	srv.AddTransport(transport.POST{})
	srv.Use(wloggqlgen.Extension())
	for _, fn := range setup {
		fn(srv)
	}
	return wlogstd.Middleware(log)(srv)
}

// post sends one GraphQL body and returns the recorded response.
func post(t *testing.T, srv http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.ServeHTTP(recorder, req)
	return recorder
}
