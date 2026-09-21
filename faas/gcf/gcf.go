// This file holds the HTTP and CloudEvent wrappers, the flush, and the Cloud Run trace.
package wloggcf

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/functions-framework-go/funcframework"
	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/propagate"
	wlogcloudevents "github.com/jeremygprawira/wlog/queue/cloudevents"
	"github.com/jeremygprawira/wlog/work"
)

// flushTimeout is the budget of the flush that runs after every invocation.
const flushTimeout = 2 * time.Second

// HTTP wraps one HTTP function with http-core, so every request gives one request event. It
// adds the Cloud Run execution id as faas.invocation_id, and it joins the trace of the
// X-Cloud-Trace-Context header. It flushes the drains before it returns, because a Cloud Run
// function freezes the process between requests. A nil Logger means wlog.Default.
func HTTP(log *wlog.Logger, fn func(http.ResponseWriter, *http.Request), opts ...httpcore.Option) func(http.ResponseWriter, *http.Request) {
	handler := httpcore.NetHTTP(log, opts...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := funcframework.ExecutionIDFromContext(r.Context()); id != "" {
			wlog.SetGroup(r.Context(), "faas", "invocation_id", id)
		}
		if traceparent := traceparentOf(r.Context()); traceparent != "" {
			r = r.WithContext(propagate.Extract(r.Context(), propagate.MapCarrier{"traceparent": traceparent}))
		}
		fn(w, r)
	}))
	return func(w http.ResponseWriter, r *http.Request) {
		defer flush(log)
		handler.ServeHTTP(w, r)
	}
}

// CloudEvent wraps one CloudEvent function, so every call gives one message event with the
// field set of the CloudEvents receiver. The framework sets up no context for a CloudEvent
// function, so the wrapper reads the ids from the event itself. It flushes the drains before it
// returns. A nil Logger means wlog.Default.
func CloudEvent(log *wlog.Logger, fn func(context.Context, cloudevents.Event) error) func(context.Context, cloudevents.Event) error {
	return func(ctx context.Context, event cloudevents.Event) error {
		defer flush(log)
		return work.Run(ctx, log, wlogcloudevents.Unit(event), func(ctx context.Context) error {
			return fn(ctx, event)
		})
	}
}

// process runs one unit of work through the event path of this adapter, with a recovered panic
// as an error, so a test continues after the panic scenario.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// flush sends the pending events of log on its own deadline, because the request context is
// often spent when the handler returns.
func flush(log *wlog.Logger) {
	if log == nil {
		log = wlog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	_ = log.Flush(ctx)
}

// traceparentOf builds a W3C traceparent from the Cloud Run trace ids of one request context,
// so the event joins the trace of the caller. The framework reads X-Cloud-Trace-Context, and
// the W3C header needs a 32 character trace id and a 16 character span id.
func traceparentOf(ctx context.Context) string {
	traceID := funcframework.TraceIDFromContext(ctx)
	if len(traceID) > 32 {
		return ""
	}
	traceID = strings.Repeat("0", 32-len(traceID)) + traceID
	span, err := strconv.ParseUint(funcframework.SpanIDFromContext(ctx), 10, 64)
	if err != nil || span == 0 {
		return ""
	}
	return "00-" + traceID + "-" + fmt.Sprintf("%016x", span) + "-01"
}
