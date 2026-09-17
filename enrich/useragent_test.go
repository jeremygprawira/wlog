package enrich_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog/enrich"
)

func TestUserAgent_Classification(t *testing.T) {
	cases := []struct {
		ua      string
		browser string
		os      string
		device  string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "Chrome", "Windows", "desktop"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0 Safari/537.36 Edg/119.0", "Edge", "Windows", "desktop"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15", "Safari", "macOS", "desktop"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0", "Firefox", "Linux", "desktop"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "Safari", "iOS", "mobile"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "Safari", "iOS", "tablet"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36", "Chrome", "Android", "mobile"},
		{"curl/8.4.0", "curl", "", "tool"},
		{"Go-http-client/1.1", "Go-http-client", "", "tool"},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "Googlebot", "", "bot"},
		{"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", "bingbot", "", "bot"},
	}
	for _, tc := range cases {
		event := map[string]any{"http": map[string]any{"user_agent": tc.ua}}
		enrich.UserAgent().Enrich(context.Background(), event)

		parsed := event["http"].(map[string]any)["user_agent_parsed"].(map[string]any)
		if parsed["browser"] != tc.browser {
			t.Errorf("%q: browser = %v, want %v", tc.ua, parsed["browser"], tc.browser)
		}
		if tc.os != "" && parsed["os"] != tc.os {
			t.Errorf("%q: os = %v, want %v", tc.ua, parsed["os"], tc.os)
		}
		if parsed["device"] != tc.device {
			t.Errorf("%q: device = %v, want %v", tc.ua, parsed["device"], tc.device)
		}
	}
}

func TestUserAgent_UnknownSetsRawOnly(t *testing.T) {
	event := map[string]any{"http": map[string]any{"user_agent": "SomeExoticClient/1.0"}}
	enrich.UserAgent().Enrich(context.Background(), event)

	parsed := event["http"].(map[string]any)["user_agent_parsed"].(map[string]any)
	if parsed["raw"] != "SomeExoticClient/1.0" {
		t.Errorf("raw = %v", parsed["raw"])
	}
	// The parse holds only raw: an unrecognized client is not guessed at.
	if _, ok := parsed["browser"]; ok {
		t.Errorf("browser = %v, want no browser for an unrecognized UA", parsed["browser"])
	}
	if len(parsed) != 1 {
		t.Errorf("parsed = %v, want raw alone", parsed)
	}
}

func TestUserAgent_NoHTTPField_IsNoop(t *testing.T) {
	event := map[string]any{}
	enrich.UserAgent().Enrich(context.Background(), event) // must not panic
	if _, ok := event["http"]; ok {
		t.Error("UserAgent created an http field out of nothing")
	}
}
