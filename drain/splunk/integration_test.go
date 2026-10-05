//go:build integration

package splunk_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/splunk"
	"github.com/jeremygprawira/wlog/pipeline"
)

// The local docker collector and its management port from docker-compose.integration.yml.
// The management port serves HTTPS with the certificate the image generates, so the search
// client skips the check on that one local host.
const (
	splunkURL = "http://localhost:8088"
	searchURL = "https://localhost:8089"
	adminUser = "admin"
	adminPass = "changeme123"
)

// searchClient talks to the local management port.
var searchClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // a local test host
	},
}

// TestSplunk_Integration proves a batch reaches Splunk Enterprise, comes back with HEC code
// 0, and answers a search for the operation it carried. It needs the compose stack, so it
// runs behind the integration tag only.
func TestSplunk_Integration(t *testing.T) {
	token := os.Getenv("SPLUNK_HEC_TOKEN")
	if token == "" {
		token = "00000000-0000-0000-0000-000000000000"
	}
	waitReady(t, splunkURL+"/services/collector/health")

	// A dropped event fails the test, because the drain always returns nil from Flush and
	// Close. Before, a collector that refused every event passed this test.
	drain, err := splunk.New(
		splunk.WithURL(splunkURL),
		splunk.WithToken(token),
		splunk.WithPipeline(pipeline.OnDropped(func(events []map[string]any, err error) {
			t.Errorf("the drain dropped %d event(s): %v", len(events), err)
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(drain))
	// The operation is fresh for every run, so an event from an earlier run cannot answer
	// the search and hide a broken send.
	operation := fmt.Sprintf("integration-op-%d", time.Now().UnixNano())
	ctx, end := wlog.Start(log.WithContext(context.Background()), operation)
	end()
	if err := log.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := log.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	waitForSearch(t, operation)
}

// waitForSearch polls a one-shot search until the operation appears, because Splunk indexes
// an event a moment after the HEC call returns.
func waitForSearch(t *testing.T, operation string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if searchFinds(t, operation) {
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("Splunk never found the operation %q", operation)
}

// searchFinds runs a one-shot search over every index and reports whether the answer holds
// the operation.
func searchFinds(t *testing.T, operation string) bool {
	t.Helper()
	form := url.Values{
		"search":      {"search index=* " + operation},
		"exec_mode":   {"oneshot"},
		"output_mode": {"json"},
	}
	req, err := http.NewRequest(http.MethodPost, searchURL+"/services/search/jobs", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build the search: %v", err)
	}
	req.SetBasicAuth(adminUser, adminPass)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := searchClient.Do(req)
	if err != nil {
		t.Logf("search: %v", err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Logf("read the search answer: %v", err)
		return false
	}
	return strings.Contains(string(body), operation)
}

// waitReady polls a URL until it answers.
func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("%s never answered", url)
}
