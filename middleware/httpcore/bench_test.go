// This file holds the realistic benchmark of the HTTP core: the number SPEC.md guards at
// 50 microseconds p50 on an M-series Mac.
package httpcore_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
)

// BenchmarkMiddleware_Realistic measures one realistic request: thirteen headers, a
// cookie, a query, a 1 KiB JSON body, safe defaults, and JSON to stdout through the async
// writer. The number excludes the drain time, because there is no drain.
func BenchmarkMiddleware_Realistic(b *testing.B) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		wlog.Set(r.Context(), "order_id", "4821")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))

	// A 1 KiB JSON body, built once so the benchmark measures the middleware alone.
	body := append([]byte(`{"note":"`), bytes.Repeat([]byte("x"), 1024-13)...)
	body = append(body, []byte(`"}`)...)

	// stdout goes to the null device, so the async writer still makes a real write call
	// without the noise of a terminal or a drain goroutine.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatalf("open %s: %v", os.DevNull, err)
	}
	original := os.Stdout
	os.Stdout = devNull

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/orders?page=2", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Accept-Language", "en-US")
		req.Header.Set("Origin", "https://shop.example")
		req.Header.Set("Referer", "https://shop.example/cart")
		req.Header.Set("Idempotency-Key", "idem-1")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "shop.example")
		req.Header.Set("X-Request-ID", "req-1")
		req.Header.Set("User-Agent", "bench/1.0")
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("X-Tenant", "t-1")
		req.Header.Set("X-Trace", "tr-1")
		req.AddCookie(&http.Cookie{Name: "sid", Value: "abc"})
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	b.StopTimer()

	os.Stdout = original
	_ = log.Close(context.Background())
	_ = devNull.Close()
}
