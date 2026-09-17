//go:build integration

package clickhouse_test

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
	"github.com/jeremygprawira/wlog/drain/clickhouse"
	"github.com/jeremygprawira/wlog/pipeline"
)

// The local docker ClickHouse from docker-compose.integration.yml.
const (
	clickhouseURL   = "http://localhost:8123"
	clickhouseTable = "wlog_integration"
)

// clickhouseReady reports whether a real ClickHouse answers on localhost:8123, so the
// test skips instead of failing when docker is not running.
func clickhouseReady() bool {
	resp, err := http.Get(clickhouseURL + "/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode == http.StatusOK && strings.Contains(string(body), "Ok")
}

// chExec runs one SQL statement over the HTTP interface and returns the response body.
func chExec(t *testing.T, query string) string {
	t.Helper()
	endpoint := clickhouseURL + "/?" + url.Values{"query": {query}}.Encode()
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		t.Fatalf("build query: %v", err)
	}
	req.SetBasicAuth("default", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("clickhouse: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clickhouse query %q: %s: %s", query, resp.Status, body)
	}
	return string(body)
}

// TestIntegration_ClickHouseReceivesEvent creates the table with DDL, sends one event
// through the drain, and waits for the row to appear.
func TestIntegration_ClickHouseReceivesEvent(t *testing.T) {
	// make integration starts the stack and waits for its health probes, so a
	// service that is not ready here is a real failure, not a reason to pass quietly.
	if !clickhouseReady() {
		t.Fatalf("ClickHouse is not ready; run make integration")
	}

	chExec(t, clickhouse.DDL("default", clickhouseTable))
	t.Cleanup(func() { chExec(t, "DROP TABLE IF EXISTS "+clickhouseTable) })

	marker := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	d, err := clickhouse.NewSender(
		clickhouse.WithURL(clickhouseURL),
		clickhouse.WithBasicAuth("default", ""),
		clickhouse.WithDatabase("default"),
		clickhouse.WithTable(clickhouseTable),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log := wlog.New(wlog.WithDrains(pipeline.Wrap(d, pipeline.BatchSize(1))))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "integration.op")
	wlog.Set(ctx, "marker", marker)
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := chExec(t, "SELECT count() FROM "+clickhouseTable+" WHERE operation = 'integration.op'")
		if strings.TrimSpace(got) != "0" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("the event did not reach ClickHouse within 30s")
}
