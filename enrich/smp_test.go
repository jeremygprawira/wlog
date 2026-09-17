package enrich_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog/enrich"
)

// TestEnrich_SMP5_GeoProvider proves Geo reads one named provider only, so a header a client
// could have sent itself is never trusted just because its name is known.
func TestEnrich_SMP5_GeoProvider(t *testing.T) {
	spoof := headersEvent(map[string]any{"CF-IPCountry": "XX"})

	// A client can send Cloudflare's header, and a CloudFront-only enricher ignores it.
	enrich.Geo("cloudfront").Enrich(context.Background(), spoof)
	if _, ok := spoof["geo"]; ok {
		t.Errorf("a Cloudflare header was trusted by the CloudFront provider: %v", spoof["geo"])
	}

	// The named provider is read, and only it.
	real := headersEvent(map[string]any{
		"CloudFront-Viewer-Country": "ID",
		"CF-IPCountry":              "XX",
	})
	enrich.Geo("cloudfront").Enrich(context.Background(), real)
	geo, _ := real["geo"].(map[string]any)
	if geo["country"] != "ID" {
		t.Errorf("geo.country = %v, want the CloudFront header", geo["country"])
	}

	// A name the package does not know trusts nothing at all.
	unknown := headersEvent(map[string]any{"CF-IPCountry": "XX"})
	enrich.Geo("not-a-cdn").Enrich(context.Background(), unknown)
	if _, ok := unknown["geo"]; ok {
		t.Errorf("an unknown provider still read a header: %v", unknown["geo"])
	}
	enrich.Geo("").Enrich(context.Background(), unknown)
	if _, ok := unknown["geo"]; ok {
		t.Errorf("an empty provider still read a header: %v", unknown["geo"])
	}

	// A custom provider needs its own header map.
	custom := headersEvent(map[string]any{"X-My-Country": "SG"})
	enrich.Geo("custom", enrich.GeoHeaders(map[string]string{"country": "X-My-Country"})).Enrich(context.Background(), custom)
	geo, _ = custom["geo"].(map[string]any)
	if geo["country"] != "SG" {
		t.Errorf("geo.country = %v, want the custom header", geo["country"])
	}
}

// TestEnrich_SMP6_NoGroupReplace proves an enricher adds context beside the app's own value
// and never replaces a group the app already recorded as something else.
func TestEnrich_SMP6_NoGroupReplace(t *testing.T) {
	// The app recorded these groups itself, as strings.
	event := map[string]any{
		"geo":  "the app's own value",
		"user": "the app's own value",
		"host": "the app's own value",
		"http": map[string]any{
			"user_agent":        "curl/8.4.0",
			"user_agent_parsed": "the app's own value",
		},
	}
	ctx := context.Background()

	enrich.Geo("cloudflare").Enrich(ctx, headersEvent(map[string]any{"CF-IPCountry": "ID"}))
	merged := map[string]any{
		"geo":  event["geo"],
		"user": event["user"],
		"host": event["host"],
	}
	enrich.User(func(context.Context) string { return "u-1" }, enrich.Overwrite(true)).Enrich(ctx, merged)
	enrich.Host().Enrich(ctx, merged)

	for _, group := range []string{"geo", "user", "host"} {
		if merged[group] != "the app's own value" {
			t.Errorf("%s = %v, want the app's own value left alone", group, merged[group])
		}
	}

	enrich.Geo("cloudflare").Enrich(ctx, event)
	if event["geo"] != "the app's own value" {
		t.Errorf("geo = %v, want the app's own value", event["geo"])
	}
	enrich.UserAgent().Enrich(ctx, event)
	if event["http"].(map[string]any)["user_agent_parsed"] != "the app's own value" {
		t.Errorf("the parse replaced the app's own value: %v", event["http"])
	}

	// Overwrite reaches inside a group the enricher owns.
	parsed := map[string]any{"http": map[string]any{
		"user_agent":        "curl/8.4.0",
		"user_agent_parsed": map[string]any{"browser": "wrong"},
	}}
	enrich.UserAgent(enrich.Overwrite(true)).Enrich(ctx, parsed)
	if got := parsed["http"].(map[string]any)["user_agent_parsed"].(map[string]any)["browser"]; got != "curl" {
		t.Errorf("Overwrite(true) left browser = %v", got)
	}
	kept := map[string]any{"http": map[string]any{
		"user_agent":        "curl/8.4.0",
		"user_agent_parsed": map[string]any{"browser": "wrong"},
	}}
	enrich.UserAgent().Enrich(ctx, kept)
	if got := kept["http"].(map[string]any)["user_agent_parsed"].(map[string]any)["browser"]; got != "wrong" {
		t.Errorf("the default replaced browser = %v", got)
	}
	// User keeps the handler's own id unless Overwrite asks for the lookup's.
	own := map[string]any{"user": map[string]any{"id": "handler-id"}}
	enrich.User(func(context.Context) string { return "lookup-id" }).Enrich(ctx, own)
	if own["user"].(map[string]any)["id"] != "handler-id" {
		t.Errorf("User replaced the handler's id: %v", own["user"])
	}
	enrich.User(func(context.Context) string { return "lookup-id" }, enrich.Overwrite(true)).Enrich(ctx, own)
	if own["user"].(map[string]any)["id"] != "lookup-id" {
		t.Errorf("User(Overwrite(true)) kept the handler's id: %v", own["user"])
	}
}

// TestEnrich_SMP7_UserAgentTable proves the table holds real clients, that each row parses to
// what it says, and that a bot and a tool are classed apart.
func TestEnrich_SMP7_UserAgentTable(t *testing.T) {
	table := []struct {
		ua      string
		browser string
		os      string
		device  string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36", "Chrome", "Windows", "desktop"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0", "Edge", "Windows", "desktop"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0", "Firefox", "Windows", "desktop"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15", "Safari", "macOS", "desktop"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1", "Safari", "iOS", "mobile"},
		{"Mozilla/5.0 (iPad; CPU OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1", "Safari", "iOS", "tablet"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36", "Chrome", "Android", "mobile"},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36", "Chrome", "Linux", "desktop"},
		{"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/27.0 Chrome/125.0.0.0 Mobile Safari/537.36", "Samsung Internet", "Android", "mobile"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 OPR/115.0.0.0", "Opera", "Windows", "desktop"},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "Googlebot", "", "bot"},
		{"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", "bingbot", "", "bot"},
		{"Mozilla/5.0 (compatible; Applebot/0.1; +http://www.apple.com/go/applebot)", "Applebot", "", "bot"},
		{"Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", "Slackbot", "", "bot"},
		{"Twitterbot/1.0", "Twitterbot", "", "bot"},
		{"facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)", "facebookexternalhit", "", "bot"},
		{"WhatsApp/2.23.20.0", "WhatsApp", "", "bot"},
		{"Discordbot/2.0; +https://discordapp.com", "Discordbot", "", "bot"},
		{"Mozilla/5.0 (compatible; LinkedInBot/1.0; +http://www.linkedin.com)", "LinkedInBot", "", "bot"},
		{"curl/8.4.0", "curl", "", "tool"},
		{"Wget/1.21.4", "Wget", "", "tool"},
		{"Go-http-client/2.0", "Go-http-client", "", "tool"},
		{"python-requests/2.32.3", "python-requests", "", "tool"},
		{"okhttp/4.12.0", "okhttp", "", "tool"},
		{"PostmanRuntime/7.39.0", "PostmanRuntime", "", "tool"},
		{"node-fetch/1.0 (+https://github.com/bitinn/node-fetch)", "node-fetch", "", "tool"},
		{"undici/6.19.0", "undici", "", "tool"},
		{"axios/1.7.0", "axios", "", "tool"},
		{"Java/17.0.10", "Java", "", "tool"},
		{"libwww-perl/6.76", "libwww-perl", "", "tool"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/131.0.6778.73 Mobile/15E148 Safari/604.1", "Chrome", "iOS", "mobile"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:133.0) Gecko/20100101 Firefox/133.0", "Firefox", "macOS", "desktop"},
	}

	// The table holds at least thirty real clients, and the parser agrees with every row.
	if len(table) < 30 {
		t.Fatalf("the test table holds %d clients, want at least 30", len(table))
	}
	for _, tc := range table {
		event := map[string]any{"http": map[string]any{"user_agent": tc.ua}}
		enrich.UserAgent().Enrich(context.Background(), event)
		parsed, _ := event["http"].(map[string]any)["user_agent_parsed"].(map[string]any)
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

// TestEnrich_SMP10_GeoTypes proves latitude and longitude arrive as numbers, so a query can
// compare them, and that a percent-encoded city, which Vercel sends, is decoded.
func TestEnrich_SMP10_GeoTypes(t *testing.T) {
	event := headersEvent(map[string]any{
		"X-Vercel-IP-Country":   "US",
		"X-Vercel-IP-City":      "New%20York",
		"X-Vercel-IP-Latitude":  "40.7128",
		"X-Vercel-IP-Longitude": "-74.0060",
	})
	enrich.Geo("vercel").Enrich(context.Background(), event)

	geo, _ := event["geo"].(map[string]any)
	if geo["city"] != "New York" {
		t.Errorf("geo.city = %v, want the decoded city", geo["city"])
	}
	if lat, ok := geo["lat"].(float64); !ok || lat != 40.7128 {
		t.Errorf("geo.lat = %v (%T), want a float", geo["lat"], geo["lat"])
	}
	if lon, ok := geo["lon"].(float64); !ok || lon != -74.006 {
		t.Errorf("geo.lon = %v (%T), want a float", geo["lon"], geo["lon"])
	}

	// A latitude that is not a number is left out instead of arriving as a string.
	bad := headersEvent(map[string]any{
		"CloudFront-Viewer-Latitude": "not-a-number",
		"CloudFront-Viewer-Country":  "ID",
	})
	enrich.Geo("cloudfront").Enrich(context.Background(), bad)
	geo, _ = bad["geo"].(map[string]any)
	if _, ok := geo["lat"]; ok {
		t.Errorf("geo.lat = %v, want no latitude for a value that is not a number", geo["lat"])
	}
	if geo["country"] != "ID" {
		t.Errorf("geo.country = %v, want the country beside it", geo["country"])
	}
}
