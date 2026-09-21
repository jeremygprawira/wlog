// This file tests the unit-of-work kit: every kind writes its group and its operation,
// the level follows the status class and the error, a panic reaches the caller with its
// stack recorded, and RecoverPanics turns it into an error.
package work_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestWork_KindGroupAndOperation proves that each kind writes its own group with the
// operation the table names.
func TestWork_KindGroupAndOperation(t *testing.T) {
	cases := []struct {
		name          string
		unit          work.Unit
		wantOperation string
		group         string
		field         any
		value         any
	}{
		{
			"message",
			work.Unit{Kind: work.KindMessage, Fields: map[string]any{
				"system": "kafka", "operation": "process", "destination": "orders",
			}},
			"process orders", "messaging", "destination", "orders",
		},
		{
			"job",
			work.Unit{Kind: work.KindJob, Fields: map[string]any{"name": "reindex"}},
			"job reindex", "job", "name", "reindex",
		},
		{
			"rpc",
			work.Unit{Kind: work.KindRPC, Fields: map[string]any{"service": "OrderService", "method": "Get"}},
			"OrderService/Get", "rpc", "method", "Get",
		},
		{
			"command",
			work.Unit{Kind: work.KindCommand, Fields: map[string]any{"path": "wlog map"}},
			"wlog map", "cli", "path", "wlog map",
		},
		{
			"function",
			work.Unit{Kind: work.KindFunction, Fields: map[string]any{"name": "handler"}},
			"function handler", "faas", "name", "handler",
		},
		{
			"work",
			work.Unit{Kind: work.KindWork, Operation: "checkout"},
			"checkout", "", "", nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t)
			_, h := work.Start(context.Background(), log, tc.unit)
			h.End(nil)

			got := rec.Last()
			if got["kind"] != string(tc.unit.Kind) {
				t.Errorf("kind = %v, want %s", got["kind"], tc.unit.Kind)
			}
			if got["operation"] != tc.wantOperation {
				t.Errorf("operation = %v, want %s", got["operation"], tc.wantOperation)
			}
			if tc.group == "" {
				return
			}
			group, ok := got[tc.group].(map[string]any)
			if !ok {
				t.Fatalf("%s group = %v, want an object", tc.group, got[tc.group])
			}
			if group[tc.field.(string)] != tc.value {
				t.Errorf("%s.%v = %v, want %v", tc.group, tc.field, group[tc.field.(string)], tc.value)
			}
		})
	}
}

// TestWork_RunErrorEvent proves that Run records the error of the handler, emits one event
// with level and outcome error, and returns the same error.
func TestWork_RunErrorEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	unit := work.Unit{Kind: work.KindJob, Fields: map[string]any{"name": "reindex"}}

	err := work.Run(context.Background(), log, unit, func(context.Context) error {
		return errors.New("reindex failed")
	})
	if err == nil || err.Error() != "reindex failed" {
		t.Fatalf("Run returned %v, want the handler error", err)
	}

	got := rec.Last()
	if got["level"] != "error" || got["outcome"] != "error" {
		t.Errorf("level/outcome = %v/%v, want error/error", got["level"], got["outcome"])
	}
	info, _ := got["error"].(map[string]any)
	if info["message"] != "reindex failed" {
		t.Errorf("error.message = %v, want the handler error", info["message"])
	}
}

// TestWork_StatusClassTables proves the shared status tables: request by code, rpc by
// gRPC code, command by exit code, and no table for a message or a job.
func TestWork_StatusClassTables(t *testing.T) {
	cases := []struct {
		kind work.Kind
		code string
		want work.StatusClass
	}{
		{work.KindRequest, "200", work.StatusOK},
		{work.KindRequest, "404", work.StatusClientError},
		{work.KindRequest, "499", work.StatusClientError},
		{work.KindRequest, "503", work.StatusServerError},
		{work.KindRPC, "OK", work.StatusOK},
		{work.KindRPC, "", work.StatusOK},
		{work.KindRPC, "NotFound", work.StatusClientError},
		{work.KindRPC, "PermissionDenied", work.StatusClientError},
		{work.KindRPC, "Unavailable", work.StatusServerError},
		{work.KindRPC, "DeadlineExceeded", work.StatusServerError},
		{work.KindCommand, "0", work.StatusOK},
		{work.KindCommand, "2", work.StatusClientError},
		{work.KindCommand, "1", work.StatusServerError},
		{work.KindMessage, "404", work.StatusOK},
		{work.KindJob, "500", work.StatusOK},
	}
	for _, tc := range cases {
		if got := work.ClassOf(tc.kind, tc.code); got != tc.want {
			t.Errorf("ClassOf(%s, %q) = %v, want %v", tc.kind, tc.code, got, tc.want)
		}
	}
}

// TestWork_ClientClassErrorIsWarn proves that an error of the client class gives warn and
// writes the code into the group, while a server class gives error.
func TestWork_ClientClassErrorIsWarn(t *testing.T) {
	log, rec := wlogtest.New(t)
	unit := work.Unit{Kind: work.KindRPC, Fields: map[string]any{"service": "OrderService", "method": "Get"}}

	_, h := work.Start(context.Background(), log, unit)
	h.Status("NotFound", work.ClassOf(work.KindRPC, "NotFound"))
	h.End(errors.New("no such order"))

	got := rec.Last()
	if got["level"] != "warn" || got["outcome"] != "success" {
		t.Errorf("level/outcome = %v/%v, want warn/success", got["level"], got["outcome"])
	}
	rpc, _ := got["rpc"].(map[string]any)
	if rpc["status_code"] != "NotFound" {
		t.Errorf("rpc.status_code = %v, want NotFound", rpc["status_code"])
	}

	// A server class with no error still gives error.
	_, h2 := work.Start(context.Background(), log, unit)
	h2.Status("Unavailable", work.ClassOf(work.KindRPC, "Unavailable"))
	h2.End(nil)
	if level := rec.Last()["level"]; level != "error" {
		t.Errorf("level = %v, want error for a server class", level)
	}

	// A 4xx ErrorInfo status gives warn even when no class was set, which is how an
	// error library reports a fault of the caller.
	statusLog, statusRec := wlogtest.New(t, wlog.WithErrorExtractor(statusExtractor{}))
	_, h3 := work.Start(context.Background(), statusLog, work.Unit{Kind: work.KindWork, Operation: "unit"})
	h3.End(errors.New("not found"))
	if level := statusRec.Last()["level"]; level != "warn" {
		t.Errorf("level = %v, want warn for a 404 error", level)
	}
}

// statusExtractor reports every error with the status 404.
type statusExtractor struct{}

// Extract returns an error detail with a client status.
func (statusExtractor) Extract(err error) wlog.ErrorInfo {
	return wlog.ErrorInfo{Message: err.Error(), Status: 404}
}

// TestWork_PanicReachesCaller proves that a panic in the handler is recorded with its
// stack, the event emits, and the panic continues.
func TestWork_PanicReachesCaller(t *testing.T) {
	log, rec := wlogtest.New(t)
	unit := work.Unit{Kind: work.KindWork, Operation: "unit"}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("the panic did not reach the caller")
		}
		if recovered != "kaboom" {
			t.Errorf("recovered %v, want the panic value", recovered)
		}
		info, _ := rec.Last()["error"].(map[string]any)
		if !strings.Contains(fmt.Sprint(info["message"]), "kaboom") {
			t.Errorf("error.message = %v, want the panic value", info["message"])
		}
		if info["stack"] == nil || info["stack"] == "" {
			t.Errorf("error.stack = %v, want the stack of the panic", info["stack"])
		}
	}()

	_ = work.Run(context.Background(), log, unit, func(context.Context) error {
		panic("kaboom")
	})
}

// TestWork_RecoverPanics proves that RecoverPanics returns the panic as an error instead of
// panicking again, and that the event still carries the stack.
func TestWork_RecoverPanics(t *testing.T) {
	log, rec := wlogtest.New(t)
	unit := work.Unit{Kind: work.KindWork, Operation: "unit"}

	err := work.Run(context.Background(), log, unit, func(context.Context) error {
		panic("kaboom")
	}, work.RecoverPanics())
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("Run returned %v, want the panic as an error", err)
	}
	info, _ := rec.Last()["error"].(map[string]any)
	if info["stack"] == nil || info["stack"] == "" {
		t.Errorf("error.stack = %v, want the stack of the panic", info["stack"])
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
}

// TestWork_ExplicitLevelWins proves that a level the handler names wins over the level the
// error asks for, so an adapter that cancels or snoozes a job keeps its own level.
func TestWork_ExplicitLevelWins(t *testing.T) {
	log, rec := wlogtest.New(t)
	unit := work.Unit{Kind: work.KindJob, Fields: map[string]any{"name": "reindex"}}

	_ = work.Run(context.Background(), log, unit, func(ctx context.Context) error {
		wlog.SetLevel(ctx, wlog.LevelWarn)
		return errors.New("job cancelled")
	})

	if level := rec.Last()["level"]; level != "warn" {
		t.Errorf("level = %v, want the level the handler named", level)
	}
}

// TestWork_CarrierLinksTrace proves that a unit with a traceparent carrier adopts the trace
// of the header, so a consumed message joins the trace of its producer.
func TestWork_CarrierLinksTrace(t *testing.T) {
	log, rec := wlogtest.New(t)
	carrier := propagate.NewBytesCarrier(map[string][]byte{
		"traceparent": []byte("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
	})

	if err := work.Run(context.Background(), log, work.Unit{Kind: work.KindMessage, Carrier: carrier}, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Run: %v", err)
	}
	trace, _ := rec.Last()["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the carrier", trace["trace_id"])
	}
	if trace["parent_span_id"] != "00f067aa0ba902b7" {
		t.Errorf("trace.parent_span_id = %v, want the span id of the carrier", trace["parent_span_id"])
	}
}
