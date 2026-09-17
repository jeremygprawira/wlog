//go:build integration

package otlp_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/otlp"
	"github.com/jeremygprawira/wlog/pipeline"
)

// reachable reports whether a TCP endpoint answers quickly. The test calls it to fail
// of failing when docker is not running.
func reachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// TestIntegration_CollectorReceivesEvent sends one event through the drain and waits
// for the collector's file exporter to write it to the shared output directory.
func TestIntegration_CollectorReceivesEvent(t *testing.T) {
	// make integration starts the stack and waits for its health probes, so a service
	// that is not ready here is a real failure, not a reason to pass quietly.
	if !reachable("localhost:4318") {
		t.Fatal("the OTel collector is not listening on localhost:4318; run make integration")
	}

	output := filepath.Join("..", "..", ".integration-out", "logs.json")
	os.Remove(output)
	marker := fmt.Sprintf("wlog-marker-%d", time.Now().UnixNano())

	d, err := otlp.NewSender(otlp.WithEndpoint("http://localhost:4318"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log := wlog.New(
		wlog.WithService("wlog-integration", "0.0.1", "test"),
		wlog.WithDrains(pipeline.Wrap(d, pipeline.BatchSize(1))),
	)

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "integration.op")
	wlog.Set(ctx, "marker", marker)
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if body, err := os.ReadFile(output); err == nil && strings.Contains(string(body), marker) {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("marker %s did not appear in %s within 30s", marker, output)
}
