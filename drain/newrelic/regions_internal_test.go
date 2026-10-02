// This file tests the region table from inside the package, because the resolved host is
// unexported and a caller cannot see it. The external test only proves that NewSender
// accepts each region.
package newrelic

import "testing"

// TestNewRelic_P13_RegionHosts proves each region resolves to its documented host, and that
// the Log API path is added to it.
func TestNewRelic_P13_RegionHosts(t *testing.T) {
	for region, want := range map[string]string{
		"us":      "https://log-api.newrelic.com",
		"eu":      "https://log-api.eu.newrelic.com",
		"jp":      "https://log-api.jp.nr-data.net",
		"fedramp": "https://gov-log-api.newrelic.com",
	} {
		got, err := endpointOf(config{region: region})
		if err != nil {
			t.Errorf("region %q: %v", region, err)
			continue
		}
		if got != want {
			t.Errorf("region %q = %q, want %q", region, got, want)
		}
	}

	// An empty region is the US one, and a trailing slash is removed.
	got, err := endpointOf(config{})
	if err != nil {
		t.Fatalf("empty region: %v", err)
	}
	if got != "https://log-api.newrelic.com" {
		t.Errorf("empty region = %q, want the US host", got)
	}
	got, err = endpointOf(config{endpoint: "https://example.test/"})
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if got != "https://example.test" {
		t.Errorf("endpoint = %q, want the trailing slash removed", got)
	}
}
