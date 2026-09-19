// This file checks the outbound HTTP transport: one request gives one call record, a
// failure records a code and never the error text, the trace headers reach the wire, and
// neither body is read by the adapter.
package wlogclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogclient "github.com/jeremygprawira/wlog/client/http"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestClient_C1_CallRecord proves that one request gives one call record with the fields
// an HTTP call needs.
func TestClient_C1_CallRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	client := newClient(t, func(*http.Request) (*http.Response, error) { return response(200, "ok"), nil })

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx = wlogclient.WithRoute(ctx, "/orders/{id}")
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/orders/42", nil)
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	end()

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	for key, want := range map[string]any{
		"kind": "http", "system": "http", "operation": "GET",
		"target": "api.example.com/orders/{id}", "status": "200",
	} {
		if record[key] != want {
			t.Errorf("call %s = %v, want %v", key, record[key], want)
		}
	}
	if _, present := record["duration_ms"]; !present {
		t.Error("call duration_ms is missing")
	}
}

// TestClient_C1_FailedRequest proves that a failed request keeps the app's error value,
// and that the record holds a code and never the error text.
func TestClient_C1_FailedRequest(t *testing.T) {
	log, rec := wlogtest.New(t)
	failure := errors.New("dial tcp 10.0.0.1: connect: connection refused")
	client := newClient(t, func(*http.Request) (*http.Response, error) { return nil, failure })

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/", nil)
	_, err := client.Do(request)
	end()

	if !errors.Is(err, failure) {
		t.Fatalf("Do returned %v, want the transport error", err)
	}
	calls, _ := rec.Last()["calls"].([]any)
	record, _ := calls[0].(map[string]any)
	info, _ := record["error"].(map[string]any)
	if info["code"] != "transport" {
		t.Errorf("call error.code = %v, want transport", info["code"])
	}
	if _, present := info["message"]; present {
		t.Errorf("call error.message = %v, want no error text", info["message"])
	}
}

// TestClient_C1_CanceledCode proves that a canceled context records its own code.
func TestClient_C1_CanceledCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	client := newClient(t, func(*http.Request) (*http.Response, error) { return nil, context.Canceled })

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/", nil)
	_, _ = client.Do(request)
	end()

	calls, _ := rec.Last()["calls"].([]any)
	record, _ := calls[0].(map[string]any)
	info, _ := record["error"].(map[string]any)
	if info["code"] != "canceled" {
		t.Errorf("call error.code = %v, want canceled", info["code"])
	}
}

// TestClient_C1_TraceHeaders proves that the request carries a traceparent with the span
// id of the call, and the request id of the context.
func TestClient_C1_TraceHeaders(t *testing.T) {
	log, _ := wlogtest.New(t)
	var traceparent, requestID, spanID string
	client := newClient(t, func(req *http.Request) (*http.Response, error) {
		traceparent = req.Header.Get("Traceparent")
		requestID = req.Header.Get("X-Request-ID")
		spanID, _ = wlog.CallSpanID(req.Context())
		return response(200, "ok"), nil
	})

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx = propagate.Extract(ctx, traceCarrier{trace: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"})
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/", nil)
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	end()

	if !strings.Contains(traceparent, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Errorf("traceparent = %q, want the incoming trace id", traceparent)
	}
	if spanID == "" || !strings.Contains(traceparent, spanID) {
		t.Errorf("traceparent = %q, want the span id %q of the call", traceparent, spanID)
	}
	if requestID == "" {
		t.Error("X-Request-ID is missing")
	}
}

// TestClient_C1_NoEvent proves that a call outside a unit of work records nothing and
// reports nothing, and that the request still goes out.
func TestClient_C1_NoEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	ran := false
	client := newClient(t, func(*http.Request) (*http.Response, error) {
		ran = true
		return response(200, "ok"), nil
	})

	request, _ := http.NewRequest(http.MethodGet, "https://api.example.com/", nil)
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	if !ran {
		t.Error("the wrapped transport never ran")
	}
	if count := len(rec.Events()); count != 0 {
		t.Errorf("events = %d, want none", count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("problems = %d, want none", count)
	}
}

// TestClient_C1_NeverReadsBody proves that the wrapper reads neither the request body nor
// the response body.
func TestClient_C1_NeverReadsBody(t *testing.T) {
	log, _ := wlogtest.New(t)
	client := newClient(t, func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("the wrapped transport read the request body: %v", err)
		}
		if string(body) != `{"order":"A-1"}` {
			t.Errorf("request body = %q, want the whole body", body)
		}
		return response(200, "reply"), nil
	})

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example.com/orders",
		strings.NewReader(`{"order":"A-1"}`))
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the response body: %v", err)
	}
	_ = resp.Body.Close()
	end()

	if string(body) != "reply" {
		t.Errorf("response body = %q, want the whole body", body)
	}
}

// TestClient_C1_TimeoutAndHostFallback proves that a timeout records its own code, and
// that a request without a URL host falls back to the request host.
func TestClient_C1_TimeoutAndHostFallback(t *testing.T) {
	if wlogclient.Transport(nil) == nil {
		t.Fatal("Transport(nil) returned nil")
	}

	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	request := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/orders"},
		Host:   "api.example.com",
		Header: http.Header{},
	}
	_, err := wlogclient.Transport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, timeoutError{}
	})).RoundTrip(request.WithContext(ctx))
	end()

	if err == nil {
		t.Fatal("RoundTrip returned no error")
	}
	calls, _ := rec.Last()["calls"].([]any)
	record, _ := calls[0].(map[string]any)
	if record["target"] != "api.example.com" {
		t.Errorf("call target = %v, want the request host", record["target"])
	}
	info, _ := record["error"].(map[string]any)
	if info["code"] != "timeout" {
		t.Errorf("call error.code = %v, want timeout", info["code"])
	}
}

// timeoutError is a net.Error that reports a timeout.
type timeoutError struct{}

// Error returns the error text.
func (timeoutError) Error() string { return "i/o timeout" }

// Timeout reports a timeout.
func (timeoutError) Timeout() bool { return true }

// Temporary reports a permanent failure.
func (timeoutError) Temporary() bool { return false }

// newClient returns one http.Client whose transport is the adapter around fn.
func newClient(t *testing.T, fn roundTripFunc) *http.Client {
	t.Helper()
	return &http.Client{Transport: wlogclient.Transport(fn)}
}

// roundTripFunc is one http.RoundTripper built from a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls fn.
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// response builds one response with a body.
func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// traceCarrier is a read-only propagate carrier over one traceparent value.
type traceCarrier struct {
	trace string
}

// Get returns the traceparent value.
func (c traceCarrier) Get(key string) string {
	if key == "traceparent" {
		return c.trace
	}
	return ""
}

// Set does nothing, because the carrier is read-only.
func (traceCarrier) Set(string, string) {}

// Keys returns the carrier keys.
func (traceCarrier) Keys() []string { return []string{"traceparent"} }
