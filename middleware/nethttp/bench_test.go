package wlogstd_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// BenchmarkMiddleware_JSONBody measures the full middleware + core overhead for a 1KB
// JSON body, no drains — SPEC.md's criterion 9: <= 50us p50, excluding drain network
// time (there is none here) and the body's own size cost.
func BenchmarkMiddleware_JSONBody(b *testing.B) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		wlog.Set(r.Context(), "order_id", "4821")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))

	body := bytes.Repeat([]byte("x"), 1024-2)
	jsonBody := append([]byte(`{"note":"`), append(body, []byte(`"}`)...)...)

	r, w, err := os.Pipe()
	if err != nil {
		b.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(drained)
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
	b.StopTimer()

	os.Stdout = orig
	_ = w.Close()
	<-drained
}
