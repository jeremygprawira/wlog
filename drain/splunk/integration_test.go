//go:build integration

package splunk_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/splunk"
)

// splunkURL is the local docker collector from docker-compose.integration.yml.
const splunkURL = "http://localhost:8088"

// TestSplunk_Integration proves a batch reaches Splunk Enterprise and comes back with HEC
// code 0. It needs the compose stack, so it runs behind the integration tag only.
func TestSplunk_Integration(t *testing.T) {
	token := os.Getenv("SPLUNK_HEC_TOKEN")
	if token == "" {
		token = "00000000-0000-0000-0000-000000000000"
	}
	waitReady(t, splunkURL+"/services/collector/health")

	drain, err := splunk.New(splunk.WithURL(splunkURL), splunk.WithToken(token))
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
}

// waitReady polls a URL until it answers.
func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // a local test URL
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("%s: the collector never became ready", url)
}
