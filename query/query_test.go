// This file checks the query language: every filter part, the comparison operators, the
// operation glob, and the errors of a bad flag.
package query_test

import (
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/query"
)

// sample is one event the tests filter.
func sample() map[string]any {
	return map[string]any{
		"level":     "error",
		"kind":      "request",
		"operation": "POST /orders/{id}",
		"timestamp": "2026-09-16T08:16:25.500Z",
		"summary":   "POST /orders/{id} 502 in 840.2ms: PAYMENT_DECLINED card declined",
		"http":      map[string]any{"status": float64(502), "route": "/orders/{id}"},
		"error":     map[string]any{"code": "PAYMENT_DECLINED"},
		"trace":     map[string]any{"trace_id": "t-1", "request_id": "r-1"},
		"event_id":  "e-1",
		"llm":       map[string]any{"cost_micros": float64(1500)},
	}
}

// compile builds one filter or fails the test.
func compile(t *testing.T, opts query.Options) *query.Filter {
	t.Helper()
	filter, err := query.Compile(opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return filter
}

// TestQuery_FilterLevelsAndKinds proves that the level and kind filters keep only the
// named values.
func TestQuery_FilterLevelsAndKinds(t *testing.T) {
	event := sample()
	keep := compile(t, query.Options{Levels: []string{"error", "warn"}, Kinds: []string{"request"}})
	if !keep.Match(event) {
		t.Error("the event did not match its own level and kind")
	}
	if compile(t, query.Options{Levels: []string{"info"}}).Match(event) {
		t.Error("an info filter matched an error event")
	}
	if compile(t, query.Options{Kinds: []string{"message"}}).Match(event) {
		t.Error("a message filter matched a request event")
	}
}

// TestQuery_FilterTimeWindow proves that since and until keep a window and refuse an
// event with no usable time.
func TestQuery_FilterTimeWindow(t *testing.T) {
	event := sample()
	since, _ := time.Parse(time.RFC3339Nano, "2026-09-16T08:00:00Z")
	until, _ := time.Parse(time.RFC3339Nano, "2026-09-16T09:00:00Z")
	if !compile(t, query.Options{Since: since, Until: until}).Match(event) {
		t.Error("the event did not match its own hour")
	}
	late, _ := time.Parse(time.RFC3339Nano, "2026-09-16T09:00:00Z")
	if compile(t, query.Options{Since: late}).Match(event) {
		t.Error("a since after the event matched")
	}
	delete(event, "timestamp")
	if compile(t, query.Options{Since: since}).Match(event) {
		t.Error("an event with no timestamp matched a time window")
	}
}

// TestQuery_FilterOperationGlob proves that one star stays in one segment and two stars
// cross a slash.
func TestQuery_FilterOperationGlob(t *testing.T) {
	event := sample()
	if !compile(t, query.Options{Operation: "POST /orders/*"}).Match(event) {
		t.Error("POST /orders/* did not match")
	}
	if compile(t, query.Options{Operation: "GET /orders/*"}).Match(event) {
		t.Error("a GET glob matched a POST event")
	}
	deep := map[string]any{"operation": "POST /orders/42/items"}
	if compile(t, query.Options{Operation: "POST /orders/*"}).Match(deep) {
		t.Error("one star crossed a slash")
	}
	if !compile(t, query.Options{Operation: "POST /orders/**"}).Match(deep) {
		t.Error("two stars did not cross a slash")
	}
}

// TestQuery_FilterStatus proves that one status filter reads the field the event has.
func TestQuery_FilterStatus(t *testing.T) {
	event := sample()
	if !compile(t, query.Options{Status: ">=500"}).Match(event) {
		t.Error(">=500 did not match a 502")
	}
	if compile(t, query.Options{Status: "<500"}).Match(event) {
		t.Error("<500 matched a 502")
	}
	rpc := map[string]any{"kind": "rpc", "rpc": map[string]any{"status_code": "OK"}}
	if !compile(t, query.Options{Status: "=OK"}).Match(rpc) {
		t.Error("=OK did not match an OK RPC call")
	}
	command := map[string]any{"kind": "command", "cli": map[string]any{"exit_code": int64(2)}}
	if !compile(t, query.Options{Status: "=2"}).Match(command) {
		t.Error("=2 did not match an exit code of 2")
	}
}

// TestQuery_FilterCodeAndIDs proves that the code and the id filters match exactly.
func TestQuery_FilterCodeAndIDs(t *testing.T) {
	event := sample()
	if !compile(t, query.Options{Code: "PAYMENT_DECLINED"}).Match(event) {
		t.Error("the code filter did not match")
	}
	if compile(t, query.Options{Code: "OTHER"}).Match(event) {
		t.Error("a wrong code matched")
	}
	if !compile(t, query.Options{TraceID: "t-1", RequestID: "r-1", EventID: "e-1"}).Match(event) {
		t.Error("the id filters did not match")
	}
	if compile(t, query.Options{RequestID: "r-2"}).Match(event) {
		t.Error("a wrong request id matched")
	}
}

// TestQuery_FilterWhere proves every operator of a where flag.
func TestQuery_FilterWhere(t *testing.T) {
	event := sample()
	cases := []struct {
		where string
		want  bool
	}{
		{"llm.cost_micros>1000", true},
		{"llm.cost_micros>=1500", true},
		{"llm.cost_micros<1000", false},
		{"llm.cost_micros<=1500", true},
		{"llm.cost_micros=1500", true},
		{"llm.cost_micros!=1500", false},
		{"http.route~^/orders", true},
		{"http.route~^/users", false},
		{"error.code?", true},
		{"missing.path?", false},
		{"level=error", true},
		{"level!=error", false},
	}
	for _, tc := range cases {
		if got := compile(t, query.Options{Where: []string{tc.where}}).Match(event); got != tc.want {
			t.Errorf("where %q matched %v, want %v", tc.where, got, tc.want)
		}
	}
}

// TestQuery_FilterText proves that the text filter reads the summary and the message,
// ignoring case.
func TestQuery_FilterText(t *testing.T) {
	event := sample()
	if !compile(t, query.Options{Text: "DECLINED"}).Match(event) {
		t.Error("text did not find the summary")
	}
	plain := map[string]any{"message": "the worker stopped"}
	if !compile(t, query.Options{Text: "WORKER"}).Match(plain) {
		t.Error("text did not find the message")
	}
	if compile(t, query.Options{Text: "refunded"}).Match(event) {
		t.Error("text matched an absent word")
	}
}

// TestQuery_CompileErrors proves that a bad where flag reports the part it cannot read.
func TestQuery_CompileErrors(t *testing.T) {
	for name, where := range map[string]string{
		"no operator": "no-operator",
		"bad regex":   "field~[",
		"empty path":  ">1",
		"empty value": "field=",
	} {
		if _, err := query.Compile(query.Options{Where: []string{where}}); err == nil {
			t.Errorf("%s: Compile returned no error", name)
		}
	}
}

// TestQuery_EmptyFilterKeepsEverything proves that no flag means no filter.
func TestQuery_EmptyFilterKeepsEverything(t *testing.T) {
	filter := compile(t, query.Options{})
	if !filter.Match(map[string]any{}) || !filter.Match(sample()) {
		t.Error("an empty filter dropped an event")
	}
}
