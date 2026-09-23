//go:build integration

package victorialogs_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/victorialogs"
)

// logsURL is the local docker VictoriaLogs from docker-compose.integration.yml.
const logsURL = "http://localhost:9428"

// TestVictoriaLogs_Integration proves a batch reaches VictoriaLogs and a query finds it.
// It needs the compose stack, so it runs behind the integration tag only.
func TestVictoriaLogs_Integration(t *testing.T) {
	waitReady(t, logsURL+"/health")

	drain, err := victorialogs.New(victorialogs.WithURL(logsURL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(drain))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "integration-op")
	end()
	if err := log.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := log.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	query := logsURL + "/select/logsql/query?query=" + url.QueryEscape(`"integration-op"`)
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		resp, err := http.Get(query) //nolint:gosec // a local test URL
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if strings.Contains(string(body), "integration-op") {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("the event never became searchable")
}

// waitReady polls a URL until it answers.
func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // a local test URL
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s: the server never became ready", url)
}
