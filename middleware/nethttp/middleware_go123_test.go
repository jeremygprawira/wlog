//go:build go1.23

// The go1.23 build tag selects this file, because it sets the Pattern field of a request,
// which net/http.Request carries from Go 1.23.
package wlogstd_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestStd_HTTP9_MuxRouteTemplate proves that the route template a ServeMux matched
// reaches the event as the operation.
func TestStd_HTTP9_MuxRouteTemplate(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/orders/42", nil)
	req.Pattern = "GET /orders/{id}"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if got := rec.Last()["operation"]; got != "GET /orders/{id}" {
		t.Errorf("operation = %v, want the mux pattern", got)
	}
}
