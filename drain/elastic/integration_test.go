//go:build integration

package elastic_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
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

// TestElastic_GoldenEvent proves the fixed event the unit golden holds reaches a live
// cluster as the same ECS document. The test reads the document back, so a mapping drift
// fails here and not only in the unit golden.
func TestElastic_GoldenEvent(t *testing.T) {
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

			sender, err := elastic.NewSender(elastic.WithURL(tc.url), elastic.WithIndex(integrationIndex))
			if err != nil {
				t.Fatalf("NewSender: %v", err)
			}
			if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent()}); err != nil {
				t.Fatalf("SendBatch: %v", err)
			}

			const id = "018f4b3c-7c00-7a00-8000-000000000000"
			got := waitForGolden(t, tc.url, id)
			want := goldenSource(t)
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				t.Errorf("the stored document drifts from the golden:\ngot %s\nwant %s", gotJSON, wantJSON)
			}
		})
	}
}

// goldenSource returns the ECS document the unit golden file holds for the fixed event.
func goldenSource(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/bulk.ndjson")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var doc map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &doc); err != nil {
		t.Fatalf("the golden line is not JSON: %v", err)
	}
	return doc
}

// waitForGolden polls the search endpoint until the document with the id is found, and
// returns its source.
func waitForGolden(t *testing.T, base, id string) map[string]any {
	t.Helper()
	url := base + "/" + integrationIndex + "/_search?q=event.id:" + id
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // a local test URL
		if err == nil {
			answer, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			var out struct {
				Hits struct {
					Hits []struct {
						Source map[string]any `json:"_source"`
					} `json:"hits"`
				} `json:"hits"`
			}
			if json.Unmarshal(answer, &out) == nil && len(out.Hits.Hits) > 0 {
				return out.Hits.Hits[0].Source
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s: the golden document never became searchable", base)
	return nil
}
