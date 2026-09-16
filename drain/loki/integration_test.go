//go:build integration

package loki_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/loki"
	"github.com/jeremygprawira/wlog/pipeline"
)

// lokiReady reports whether a real Loki answers on localhost:3100, so the test skips
// instead of failing when docker is not running or another program owns the port.
func lokiReady() bool {
	resp, err := http.Get("http://localhost:3100/ready")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// lokiHasMarker queries Loki for the test service and reports whether the marker
// string is already ingested.
func lokiHasMarker(t *testing.T, marker string) bool {
	t.Helper()
	query := url.Values{}
	query.Set("query", `{service="wlog-integration"}`)
	query.Set("limit", "100")
	resp, err := http.Get("http://localhost:3100/loki/api/v1/query_range?" + query.Encode())
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	return strings.Contains(string(body), marker)
}

// TestIntegration_LokiReceivesEvent sends one event through the drain and waits for
// Loki to return it from a query.
func TestIntegration_LokiReceivesEvent(t *testing.T) {
	if !lokiReady() {
		t.Skip("Loki is not ready on localhost:3100, start it with make integration")
	}

	d, err := loki.New(loki.WithURL("http://localhost:3100"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log := wlog.New(
		wlog.WithService("wlog-integration", "0.0.1", "test"),
		wlog.WithDrains(pipeline.Wrap(d, pipeline.BatchSize(1))),
	)
	marker := fmt.Sprintf("wlog-marker-%d", time.Now().UnixNano())

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "integration.op")
	wlog.Set(ctx, "marker", marker)
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if lokiHasMarker(t, marker) {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("marker %s did not appear in Loki within 30s", marker)
}
