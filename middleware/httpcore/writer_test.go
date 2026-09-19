// This file tests the panic policy and the response writer wrapper black box: a panic
// still emits, http.ErrAbortHandler reaches net/http, and a streamed body is not held.
package httpcore_test

import (
	"io"
	stdlog "log"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestHTTPCore_HTTP6_WriterMethods proves that a flush before a write counts as status
// 200, that a successful hijack records 101, and that a response body is captured.
func TestHTTPCore_HTTP6_WriterMethods(t *testing.T) {
	flushed, flushRec := wlogtest.New(t)
	flushHandler := httpcore.NetHTTP(flushed)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		w.WriteHeader(http.StatusCreated)
	}))
	flushHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if status := httpFields(t, flushRec.Last())["status"]; status != int64(200) {
		t.Errorf("status after a flush before the first write = %v, want 200", status)
	}

	hijacked, hijackRec := wlogtest.New(t)
	hijackHandler := httpcore.NetHTTP(hijacked)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = buffer.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		_ = buffer.Flush()
		_ = conn.Close()
	}))
	server := httptest.NewServer(hijackHandler)
	defer server.Close()
	if resp, err := server.Client().Get(server.URL); err == nil {
		_ = resp.Body.Close()
	}
	// The client can return before the handler goroutine emits, so wait for the event.
	for deadline := time.Now().Add(2 * time.Second); hijackRec.Count() == 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if status := httpFields(t, hijackRec.Last())["status"]; status != int64(101) {
		t.Errorf("status after a hijack = %v, want 101", status)
	}

	captured, captureRec := wlogtest.New(t)
	captureHandler := httpcore.NetHTTP(captured, httpcore.CaptureAll())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"order_id":"A-1"}`))
		}))
	captureHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	body, ok := httpFields(t, captureRec.Last())["response_body"].(map[string]any)
	if !ok {
		t.Fatalf("response_body = %v, want the parsed body", httpFields(t, captureRec.Last())["response_body"])
	}
	if body["order_id"] != "A-1" {
		t.Errorf("response_body order_id = %v, want A-1", body["order_id"])
	}
}

// TestHTTPCore_HTTP6_PanicPolicies proves that Recover500 answers with a 500 and keeps
// the event with the panic stack, and that Repanic emits and lets the panic continue.
func TestHTTPCore_HTTP6_PanicPolicies(t *testing.T) {
	boom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") })

	log, rec := wlogtest.New(t)
	server := httptest.NewServer(httpcore.NetHTTP(log)(boom))
	defer server.Close()

	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if rec.Count() != 1 {
		t.Fatalf("events = %d, want 1", rec.Count())
	}
	info, ok := rec.Last()["error"].(map[string]any)
	if !ok {
		t.Fatalf("the event carries no error: %v", rec.Last())
	}
	if message, _ := info["message"].(string); !strings.Contains(message, "boom") {
		t.Errorf("error.message = %q, want the panic value", message)
	}
	if stack, _ := info["stack"].(string); stack == "" {
		t.Error("the panic was recorded without a stack")
	}

	repanicked, reRec := wlogtest.New(t)
	quiet := httptest.NewServer(httpcore.NetHTTP(repanicked, httpcore.PanicPolicy(httpcore.Repanic))(boom))
	quiet.Config.ErrorLog = stdlog.New(io.Discard, "", 0)
	defer quiet.Close()

	if _, err := quiet.Client().Get(quiet.URL); err == nil {
		t.Error("Repanic returned no error to the client")
	}
	if reRec.Count() != 1 {
		t.Errorf("events = %d, want the event of the repanicked request", reRec.Count())
	}
}

// TestHTTPCore_HTTP6_AbortHandlerRepanics proves that a handler which panics with
// http.ErrAbortHandler still emits its event, and that the panic reaches net/http.
func TestHTTPCore_HTTP6_AbortHandlerRepanics(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	if _, err := server.Client().Get(server.URL); err == nil {
		t.Error("the aborted request returned no error to the client")
	}
	if rec.Count() != 1 {
		t.Fatalf("events = %d, want the event of the aborted request", rec.Count())
	}
	if rec.Last()["outcome"] != "error" {
		t.Errorf("outcome = %v, want error", rec.Last()["outcome"])
	}
}

// TestHTTPCore_HTTP6_ResponseControllerDeadline proves that http.ResponseController
// reaches the real writer through the wrapper, so a deadline still applies.
func TestHTTPCore_HTTP6_ResponseControllerDeadline(t *testing.T) {
	log, _ := wlogtest.New(t)
	errs := make(chan error, 1)
	handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errs <- http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
		w.WriteHeader(http.StatusOK)
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if err := <-errs; err != nil {
		t.Errorf("SetWriteDeadline through the wrapper: %v", err)
	}
}

// TestHTTPCore_HTTP7_ReadFromAllocations proves that a streamed download adds almost no
// allocation, because the wrapper hands the stream to the real writer.
func TestHTTPCore_HTTP7_ReadFromAllocations(t *testing.T) {
	const size = 64 << 20
	download := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, io.LimitReader(zeroReader{}, size))
	})

	plain := httptest.NewServer(download)
	defer plain.Close()
	log, _ := wlogtest.New(t)
	wrapped := httptest.NewServer(httpcore.NetHTTP(log)(download))
	defer wrapped.Close()

	plainAlloc := downloadAlloc(t, plain.URL)
	wrappedAlloc := downloadAlloc(t, wrapped.URL)
	if wrappedAlloc > plainAlloc+(2<<20) {
		t.Errorf("the wrapped download allocated %d bytes over the plain one, want under 2 MiB",
			wrappedAlloc-plainAlloc)
	}
}

// downloadAlloc returns the bytes the heap allocated while one download was served and
// drained.
func downloadAlloc(t *testing.T, url string) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// zeroReader returns zero bytes forever, so the download needs no fixture.
type zeroReader struct{}

// Read fills the buffer with zero bytes.
func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
