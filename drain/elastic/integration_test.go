//go:build integration

package elastic_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/elastic"
)

// The local docker clusters from docker-compose.integration.yml.
const (
	elasticURL       = "http://localhost:9200"
	openSearchURL    = "http://localhost:9201"
	integrationIndex = "logs-wlog-integration"
)

// TestElastic_Integration proves the template installs and a sent event is searchable on
// Elasticsearch 8 and OpenSearch 2. It needs the compose stack, so it runs behind the
// integration tag only.
func TestElastic_Integration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		url    string
		engine elastic.Engine
	}{
		{"elasticsearch", elasticURL, elastic.Elasticsearch},
		{"opensearch", openSearchURL, elastic.OpenSearch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			waitReady(t, tc.url)
			installTemplate(t, tc.url, tc.engine)

			drain, err := elastic.New(elastic.WithURL(tc.url), elastic.WithIndex(integrationIndex))
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
			waitForEvent(t, tc.url)
		})
	}
}

// waitReady polls the cluster health until it answers yellow or better.
func waitReady(t *testing.T, base string) {
	t.Helper()
	url := base + "/_cluster/health?wait_for_status=yellow&timeout=60s"
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // a local test URL
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s: the cluster never became ready", base)
}

// installTemplate puts the composable index template the drain ships.
func installTemplate(t *testing.T, base string, engine elastic.Engine) {
	t.Helper()
	body := elastic.Template(engine)
	req, err := http.NewRequest(http.MethodPut, base+"/_index_template/logs-wlog", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("install template: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		answer, _ := io.ReadAll(resp.Body)
		t.Fatalf("install template: status %d: %s", resp.StatusCode, answer)
	}
}

// waitForEvent polls the search endpoint until the operation is found.
func waitForEvent(t *testing.T, base string) {
	t.Helper()
	url := base + "/" + integrationIndex + "/_search?q=event.action:integration-op"
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // a local test URL
		if err == nil {
			answer, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			var out struct {
				Hits struct {
					Total struct {
						Value int `json:"value"`
					} `json:"total"`
				} `json:"hits"`
			}
			if json.Unmarshal(answer, &out) == nil && out.Hits.Total.Value > 0 {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s: the event never became searchable", base)
}
