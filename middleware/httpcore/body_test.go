// This file tests request body capture black box: a JSON body of any shape, a body cut
// at the cap, a form body, and the cap itself.
package httpcore_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestHTTPCore_HTTP1_CutBodyNoText proves that a body cut at the cap becomes the
// truncated marker, that no text from it is kept, and that the handler still reads it.
func TestHTTPCore_HTTP1_CutBodyNoText(t *testing.T) {
	const secret = "sup3r-s3cret-value"
	payload := `{"secret":"` + secret + `","pad":"` + strings.Repeat("x", 200) + `"}`

	var readByHandler string
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log, httpcore.CaptureAll(), httpcore.MaxBody(32))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			readByHandler = string(body)
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readByHandler != payload {
		t.Errorf("the handler read %d bytes, want the whole body", len(readByHandler))
	}
	body, ok := httpFields(t, rec.Last())["request_body"].(map[string]any)
	if !ok {
		t.Fatalf("request_body = %v, want the truncated marker", httpFields(t, rec.Last())["request_body"])
	}
	if body["truncated"] != true || body["bytes"] != int64(32) {
		t.Errorf("request_body = %v, want the marker for 32 bytes", body)
	}
	if event := eventJSON(t, rec.Last()); strings.Contains(event, secret) || strings.Contains(event, "xxx") {
		t.Errorf("the cut body kept its text: %s", event)
	}
}

// TestHTTPCore_HTTP1_FormPassword proves that a form body parses into key and value
// pairs, so a key rule redacts the password field.
func TestHTTPCore_HTTP1_FormPassword(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log, httpcore.CaptureAll())(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("user=jane&password=hunter2"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	body, ok := httpFields(t, rec.Last())["request_body"].(map[string]any)
	if !ok {
		t.Fatalf("request_body = %v, want the parsed form", httpFields(t, rec.Last())["request_body"])
	}
	if body["user"] != "jane" {
		t.Errorf("request_body user = %v, want jane", body["user"])
	}
	if body["password"] == "hunter2" {
		t.Errorf("the password was kept: %v", body["password"])
	}
}

// TestHTTPCore_HTTP16_MaxBodyClamp proves that a negative cap clamps to zero, that a huge
// cap clamps to one MiB, and that a small body reuses the pooled buffer.
func TestHTTPCore_HTTP16_MaxBodyClamp(t *testing.T) {
	var readByHandler string
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log, httpcore.CaptureAll(), httpcore.MaxBody(-1))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			readByHandler = string(body)
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if readByHandler != `{"a":1}` {
		t.Errorf("the handler read %q, want the whole body", readByHandler)
	}
	if _, kept := httpFields(t, rec.Last())["request_body"]; kept {
		t.Errorf("a zero cap captured a body: %v", httpFields(t, rec.Last())["request_body"])
	}

	big := `{"pad":"` + strings.Repeat("x", 1<<20) + `"}`
	huge, hugeRec := wlogtest.New(t)
	hugeHandler := httpcore.NetHTTP(huge, httpcore.CaptureAll(), httpcore.MaxBody(4<<20))(okHandler())
	hugeReq := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(big))
	hugeReq.Header.Set("Content-Type", "application/json")
	hugeHandler.ServeHTTP(httptest.NewRecorder(), hugeReq)
	body, _ := httpFields(t, hugeRec.Last())["request_body"].(map[string]any)
	if body["bytes"] != int64(1<<20) {
		t.Errorf("a cap beyond the limit captured %v bytes, want one MiB", body["bytes"])
	}

	// A small body must not allocate the cap. A collection clears a sync.Pool, so the
	// measurement turns the collector off: with it off, a later request reuses the buffer
	// the first one put back. Without the pool every request allocates the cap.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	pooled, _ := wlogtest.New(t)
	pooledHandler := httpcore.NetHTTP(pooled, httpcore.CaptureAll(), httpcore.MaxBody(1<<20))(okHandler())
	best := ^uint64(0)
	for i := 0; i < 10; i++ {
		best = min(best, allocatedBy(t, pooledHandler, 1))
	}
	if best > 256<<10 {
		t.Errorf("the smallest of ten small bodies allocated %d bytes, want the pooled buffer", best)
	}
}

// allocatedBy returns the bytes the heap allocated while the handler served n requests
// with a small JSON body. A warm-up request runs first, so the pool already holds a
// buffer when the measurement starts.
func allocatedBy(t *testing.T, handler http.Handler, n int) uint64 {
	t.Helper()
	serveSmallBody(handler)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < n; i++ {
		serveSmallBody(handler)
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// serveSmallBody serves one request with a small JSON body.
func serveSmallBody(handler http.Handler) {
	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

// TestHTTPCore_HTTP1_ArrayBodyRedacted proves that a JSON array body reaches the event as
// a tree, that a key rule redacts inside it, and that the handler still reads the whole
// body.
func TestHTTPCore_HTTP1_ArrayBodyRedacted(t *testing.T) {
	const payload = `[{"password":"hunter2"},{"order_id":"A-1"}]`
	var readByHandler string
	log, rec := wlogtest.New(t)
	handler := httpcore.NetHTTP(log, httpcore.CaptureAll())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			readByHandler = string(body)
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readByHandler != payload {
		t.Errorf("the handler read %q, want the whole body", readByHandler)
	}
	body, ok := httpFields(t, rec.Last())["request_body"].([]any)
	if !ok {
		t.Fatalf("request_body = %v, want the parsed array", httpFields(t, rec.Last())["request_body"])
	}
	if len(body) != 2 {
		t.Fatalf("request_body holds %d elements, want 2", len(body))
	}
	if body := eventJSON(t, rec.Last()); strings.Contains(body, "hunter2") {
		t.Errorf("the password reached the event: %s", body)
	}
	if !strings.Contains(eventJSON(t, rec.Last()), "A-1") {
		t.Errorf("the array body lost its second element: %s", eventJSON(t, rec.Last()))
	}
}
