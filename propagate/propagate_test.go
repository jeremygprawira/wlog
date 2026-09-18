// This file tests the propagation of one trace context: the W3C traceparent vectors, the
// request id rule, the read-only B3 and X-Ray fallbacks, the trace group of an event, and
// the span id of an open call.
package propagate_test

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// hex32 matches a trace id, and hex16 a span id.
var (
	hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex16 = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// TestPropagate_W3CVectors proves the traceparent rules of W3C Trace Context level 1, and
// that Extract followed by Inject round-trips the trace id with a new span id.
//
// The vectors come from the rules and the examples of the W3C Trace Context
// recommendation, section "traceparent header".
func TestPropagate_W3CVectors(t *testing.T) {
	const (
		traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		spanID  = "00f067aa0ba902b7"
	)
	cases := []struct {
		name    string
		header  string
		valid   bool
		sampled bool
	}{
		{"version 00 sampled", "00-" + traceID + "-" + spanID + "-01", true, true},
		{"version 00 not sampled", "00-" + traceID + "-" + spanID + "-00", true, false},
		{"version 01", "01-" + traceID + "-" + spanID + "-01", true, true},
		{"future version with extra fields", "cc-" + traceID + "-" + spanID + "-01-extra", true, true},
		{"version ff", "ff-" + traceID + "-" + spanID + "-01", false, false},
		{"zero trace id", "00-00000000000000000000000000000000-" + spanID + "-01", false, false},
		{"zero span id", "00-" + traceID + "-0000000000000000-01", false, false},
		{"too few fields", "00-" + traceID + "-" + spanID, false, false},
		{"version 00 with an extra field", "00-" + traceID + "-" + spanID + "-01-extra", false, false},
		{"short trace id", "00-4bf92f3577b34da6a3ce929d0e0e473-" + spanID + "-01", false, false},
		{"uppercase hex", "00-4BF92F3577B34DA6A3CE929D0E0E4736-" + spanID + "-01", false, false},
		{"flags that are not hex", "00-" + traceID + "-" + spanID + "-zz", false, false},
		{"empty header", "", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			incoming := propagate.HeaderCarrier{"Traceparent": {tc.header}}
			tc2, ok := propagate.FromContext(propagate.Extract(context.Background(), incoming))
			if tc.valid {
				if !ok || tc2.TraceID != traceID || tc2.ParentSpanID != spanID || tc2.Sampled != tc.sampled {
					t.Fatalf("Extract(%q) = %+v, want trace %s with parent %s", tc.header, tc2, traceID, spanID)
				}
			} else {
				if tc2.TraceID == traceID {
					t.Fatalf("Extract(%q) kept a trace id it must refuse", tc.header)
				}
				if tc2.ParentSpanID != "" {
					t.Errorf("Extract(%q) kept parent span %q", tc.header, tc2.ParentSpanID)
				}
			}
			if !hex32.MatchString(tc2.TraceID) {
				t.Errorf("trace id = %q, want 32 lowercase hex characters", tc2.TraceID)
			}
			if !hex16.MatchString(tc2.SpanID) {
				t.Errorf("span id = %q, want 16 lowercase hex characters", tc2.SpanID)
			}

			// Inject round-trips the trace id and writes a new span id.
			outgoing := propagate.HeaderCarrier{}
			propagate.Inject(propagate.ContextWith(context.Background(), tc2), outgoing)
			round, ok := propagate.FromContext(propagate.Extract(context.Background(), outgoing))
			if !ok || round.TraceID != tc2.TraceID {
				t.Errorf("round trip trace id = %q, want %q", round.TraceID, tc2.TraceID)
			}
			if round.ParentSpanID != tc2.SpanID {
				t.Errorf("round trip parent span = %q, want the injected span %q", round.ParentSpanID, tc2.SpanID)
			}
		})
	}
}

// TestPropagate_RequestIDRule proves that X-Request-ID counts only at 128 characters or
// fewer from [A-Za-z0-9._:-], and that RequestID is the trace id otherwise.
func TestPropagate_RequestIDRule(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"a plain id", "8a3692537f716fa0", "8a3692537f716fa0"},
		{"the allowed punctuation", "a.b_c:d-e", "a.b_c:d-e"},
		{"at the limit", strings.Repeat("a", 128), strings.Repeat("a", 128)},
		{"past the limit", strings.Repeat("a", 129), traceID},
		{"a space", "has a space", traceID},
		{"a slash", "abc/def", traceID},
		{"absent", "", traceID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			incoming := propagate.HeaderCarrier{
				"Traceparent":  {"00-" + traceID + "-00f067aa0ba902b7-01"},
				"X-Request-Id": {tc.header},
			}
			got, _ := propagate.FromContext(propagate.Extract(context.Background(), incoming))
			if got.RequestID != tc.want {
				t.Errorf("RequestID = %q, want %q", got.RequestID, tc.want)
			}
		})
	}
}

// TestPropagate_B3XRayOptions proves that a B3 header and an X-Ray header each name the
// trace with their option, and nothing without it.
func TestPropagate_B3XRayOptions(t *testing.T) {
	const (
		traceID = "80f198ee56343ba864fe8b2a57d3eff7"
		spanID  = "e457b5a2e4d86bd1"
		xrayID  = "5759e988bd862e3fe1be46a994272793"
	)
	b3 := propagate.HeaderCarrier{
		"B3":           {traceID + "-" + spanID + "-1"},
		"X-B3-Traceid": {traceID},
		"X-B3-Spanid":  {spanID},
	}
	multi := propagate.HeaderCarrier{
		"X-B3-Traceid": {traceID},
		"X-B3-Spanid":  {spanID},
		"X-B3-Sampled": {"1"},
	}
	xray := propagate.HeaderCarrier{
		"X-Amzn-Trace-Id": {"Root=1-" + "5759e988" + "-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Sampled=1"},
	}

	withB3, _ := propagate.FromContext(propagate.Extract(context.Background(), b3, propagate.WithB3()))
	if withB3.TraceID != traceID || withB3.ParentSpanID != spanID || !withB3.Sampled {
		t.Errorf("the b3 single header gave %+v, want trace %s", withB3, traceID)
	}
	withMulti, _ := propagate.FromContext(propagate.Extract(context.Background(), multi, propagate.WithB3()))
	if withMulti.TraceID != traceID || !withMulti.Sampled {
		t.Errorf("the b3 multi headers gave %+v, want trace %s", withMulti, traceID)
	}
	withXRay, _ := propagate.FromContext(propagate.Extract(context.Background(), xray, propagate.WithXRay()))
	if withXRay.TraceID != xrayID || !withXRay.Sampled {
		t.Errorf("the X-Ray header gave %+v, want trace %s", withXRay, xrayID)
	}

	// Without the option, each fallback stays unread.
	without, _ := propagate.FromContext(propagate.Extract(context.Background(), b3))
	if without.TraceID == traceID {
		t.Error("the b3 header was read without WithB3")
	}
	withoutXRay, _ := propagate.FromContext(propagate.Extract(context.Background(), xray))
	if withoutXRay.TraceID == xrayID {
		t.Error("the X-Ray header was read without WithXRay")
	}
}

// TestPropagate_ContextWithUpdatesEvent proves that ContextWith replaces the ids on the
// context and in the trace group of the event, which is how a recording span wins over
// the ids that Extract made.
func TestPropagate_ContextWithUpdatesEvent(t *testing.T) {
	const (
		traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		spanID  = "00f067aa0ba902b7"
	)
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "unit")

	ctx = propagate.ContextWith(ctx, propagate.TraceContext{
		TraceID: traceID, SpanID: spanID, RequestID: "req-1",
	})
	end()

	trace, ok := rec.Last()["trace"].(map[string]any)
	if !ok {
		t.Fatalf("trace = %v, want the group", rec.Last()["trace"])
	}
	if trace["trace_id"] != traceID || trace["span_id"] != spanID || trace["request_id"] != "req-1" {
		t.Errorf("trace group = %v, want the ids ContextWith replaced", trace)
	}
	if got, _ := propagate.FromContext(ctx); got.TraceID != traceID || got.SpanID != spanID {
		t.Errorf("context trace = %+v, want the ids ContextWith replaced", got)
	}
}

// TestPropagate_InjectUsesCallSpan proves that Inject inside a call writes the span id of
// that call, so the next service links to the call and not to the whole unit of work.
func TestPropagate_InjectUsesCallSpan(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "unit")
	// A request extracts the trace of its caller first, which is what gives Inject the
	// trace id and the request id to write.
	incoming := propagate.HeaderCarrier{
		"Traceparent":  {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		"X-Request-Id": {"req-1"},
	}
	ctx = propagate.Extract(ctx, incoming)

	callCtx, callEnd := wlog.StartCall(ctx, wlog.Call{Kind: "http", Operation: "GET", Target: "api.example.com"})
	callSpan, ok := wlog.CallSpanID(callCtx)
	if !ok {
		t.Fatal("CallSpanID returned no span id")
	}

	outgoing := propagate.HeaderCarrier{}
	propagate.Inject(callCtx, outgoing)
	callEnd(wlog.CallResult{Status: "200"})
	end()

	header := outgoing.Get("traceparent")
	if !strings.Contains(header, "-"+callSpan+"-") {
		t.Errorf("traceparent = %q, want the call span %s", header, callSpan)
	}
	if !strings.Contains(header, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Errorf("traceparent = %q, want the incoming trace id", header)
	}
	if outgoing.Get("X-Request-ID") != "req-1" {
		t.Errorf("X-Request-ID = %q, want req-1", outgoing.Get("X-Request-ID"))
	}

	calls, _ := rec.Last()["calls"].([]any)
	record, _ := calls[0].(map[string]any)
	if record["span_id"] != callSpan {
		t.Errorf("call record span_id = %v, want the injected span %s", record["span_id"], callSpan)
	}
}

// TestPropagate_Carriers proves the three carriers read and write one key, and that only
// a HeaderCarrier ignores case.
func TestPropagate_Carriers(t *testing.T) {
	headers := propagate.HeaderCarrier(http.Header{})
	headers.Set("traceparent", "one")
	if got := headers.Get("TraceParent"); got != "one" {
		t.Errorf("HeaderCarrier.Get ignored case = %q, want one", got)
	}
	if keys := headers.Keys(); len(keys) != 1 {
		t.Errorf("HeaderCarrier.Keys = %v, want one key", keys)
	}

	plain := propagate.MapCarrier{}
	plain.Set("traceparent", "two")
	if plain.Get("TraceParent") != "" {
		t.Error("MapCarrier matched a key without regard to case")
	}
	if got := plain.Get("traceparent"); got != "two" {
		t.Errorf("MapCarrier.Get = %q, want two", got)
	}

	bytes := propagate.NewBytesCarrier(nil)
	bytes.Set("traceparent", "three")
	if got := bytes.Get("traceparent"); got != "three" {
		t.Errorf("BytesCarrier.Get = %q, want three", got)
	}
	if keys := bytes.Keys(); len(keys) != 1 || keys[0] != "traceparent" {
		t.Errorf("BytesCarrier.Keys = %v, want traceparent", keys)
	}
}
