// This file runs the work conformance suite against the CloudEvent path, and checks the HTTP
// and CloudEvent wrappers.
package wloggcf

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/functions-framework-go/funcframework"
	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestGcf_C1_WorkConformance proves that the CloudEvent path passes every scenario of the work
// suite.
func TestGcf_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path of this adapter. The suite supplies the
// unit, because one CloudEvent carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestGcf_C1_CloudEventRecordsTheEvent proves that one CloudEvent call records the messaging
// group with the fields of the CloudEvents receiver, and the trace of its extension.
func TestGcf_C1_CloudEventRecordsTheEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	event := cloudevents.NewEvent()
	event.SetID("evt-1")
	event.SetSource("orders")
	event.SetType("orders.created")
	event.SetSubject("orders.created")
	event.SetTime(time.Now().Add(-2 * time.Second))
	event.SetExtension("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	if err := CloudEvent(log, func(context.Context, cloudevents.Event) error { return nil })(context.Background(), event); err != nil {
		t.Fatalf("the function returned %v", err)
	}

	got := lastEvent(t, rec)
	if got["kind"] != "message" || got["operation"] != "process orders.created" {
		t.Errorf("kind/operation = %v/%v, want message/process orders.created", got["kind"], got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	ce, _ := messaging["cloudevents"].(map[string]any)
	for key, want := range map[string]any{
		"event_id": "evt-1", "event_source": "orders", "event_type": "orders.created",
	} {
		if ce[key] != want {
			t.Errorf("messaging.cloudevents.%s = %v, want %v", key, ce[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the extension", trace["trace_id"])
	}
}

// TestGcf_C1_CloudEventPanicRecordsStack proves that a panicking CloudEvent function records
// one error event with a stack, and the panic continues.
func TestGcf_C1_CloudEventPanicRecordsStack(t *testing.T) {
	log, rec := wlogtest.New(t)

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the wrapper did not panic again")
			}
		}()
		_ = CloudEvent(log, func(context.Context, cloudevents.Event) error { panic("boom") })(context.Background(), cloudevents.NewEvent())
	}()

	info, _ := lastEvent(t, rec)["error"].(map[string]any)
	if info == nil || info["stack"] == nil {
		t.Errorf("error = %v, want the recovered stack", info)
	}
}

// TestGcf_C1_BothFlushBeforeReturn proves that both wrappers deliver their event through a
// pipeline before they return.
func TestGcf_C1_BothFlushBeforeReturn(t *testing.T) {
	sender := &fakeSender{}
	log := wlog.New(
		wlog.WithSilent(),
		wlog.WithService("gcf-test", "0.0.1", "prod"),
		wlog.WithDrains(pipeline.Wrap(sender)),
	)

	rec := httptest.NewRecorder()
	HTTP(log, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if err := CloudEvent(log, func(context.Context, cloudevents.Event) error { return nil })(context.Background(), cloudevents.NewEvent()); err != nil {
		t.Fatalf("the CloudEvent function returned %v", err)
	}

	if count := sender.count(); count != 2 {
		t.Errorf("delivered %d events, want one per wrapper", count)
	}
}

// TestGcf_C1_HTTPRecordsExecutionIDAndTrace proves that the framework dispatch gives the
// event the execution id of the request and the trace of X-Cloud-Trace-Context.
func TestGcf_C1_HTTPRecordsExecutionIDAndTrace(t *testing.T) {
	t.Setenv("FUNCTION_TARGET", "gcf-test")
	log, rec := wlogtest.New(t)
	handler := HTTP(log, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	if err := funcframework.RegisterHTTPFunctionContext(context.Background(), "/", handler); err != nil {
		t.Fatalf("register the function: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	go func() { _ = funcframework.StartHostPort("127.0.0.1", strconv.Itoa(port)) }()

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/"
	if err := waitForServer(url); err != nil {
		t.Fatalf("the server did not start: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	request.Header.Set("Function-Execution-Id", "exec-1")
	request.Header.Set("X-Cloud-Trace-Context", "105445aa7843bc8bf206b12000100000/1;o=1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	_ = response.Body.Close()

	got := waitForExecution(t, rec, "exec-1")
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "105445aa7843bc8bf206b12000100000" {
		t.Errorf("trace.trace_id = %v, want the trace id of the header", trace["trace_id"])
	}
	if trace["parent_span_id"] != "0000000000000001" {
		t.Errorf("trace.parent_span_id = %v, want the span id of the header", trace["parent_span_id"])
	}
}

// waitForExecution polls the recorder until the event of one execution id lands, because the
// server may emit the event after the client reads the response.
func waitForExecution(t *testing.T, rec *wlogtest.Recorder, executionID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range rec.Events() {
			faas, _ := event["faas"].(map[string]any)
			if faas["invocation_id"] == executionID {
				return event
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no event with execution id %q in %d events", executionID, len(rec.Events()))
	return nil
}

// waitForServer polls one URL until the server answers or the deadline passes.
func waitForServer(url string) error {
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		request, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			_ = response.Body.Close()
			return nil
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	return lastErr
}

// lastEvent returns the only recorded event, and stops the test when the run recorded none.
func lastEvent(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	return got
}
