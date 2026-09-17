package enrich

import (
	"context"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// UserAgent parses http.user_agent into http.user_agent_parsed.{raw,browser,os,device}. A
// real client the table holds is named exactly; another string is classified by its tokens;
// and a string that matches nothing keeps only raw, because a guess about a client is worse
// than no guess.
//
// UserAgent(Overwrite(true)) replaces a parse an earlier stage already made.
func UserAgent(opts ...Option) wlog.Enricher {
	cfg := newConfig(opts)
	return wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
		httpField, ok := event["http"].(map[string]any)
		if !ok {
			return
		}
		ua, _ := httpField["user_agent"].(string)
		if ua == "" {
			return
		}
		parsed := parseUserAgent(ua)
		if existing, ok := httpField["user_agent_parsed"]; ok && existing != nil {
			group, isMap := existing.(map[string]any)
			if !isMap {
				// The app recorded its own value under this key, and an enricher never
				// replaces what the app itself wrote (gate G3).
				return
			}
			for key, value := range parsed {
				if _, exists := group[key]; exists && !cfg.overwrite {
					continue
				}
				group[key] = value
			}
			return
		}
		httpField["user_agent_parsed"] = parsed
	})
}

// userAgent is what one table row says about a client.
type userAgent struct {
	browser string
	os      string
	device  string
}

// userAgentTable holds real user-agent strings from the major browsers, the common crawlers,
// and the common HTTP clients, so a real client is named exactly instead of being guessed at
// from its tokens.
var userAgentTable = map[string]userAgent{
	// Chrome.
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36":                                {"Chrome", "Windows", "desktop"},
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36":                          {"Chrome", "macOS", "desktop"},
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36":                                          {"Chrome", "Linux", "desktop"},
	"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36":                          {"Chrome", "Android", "mobile"},
	"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/131.0.6778.73 Mobile/15E148 Safari/604.1": {"Chrome", "iOS", "mobile"},

	// Edge, Firefox, Safari, and Samsung Internet.
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0":              {"Edge", "Windows", "desktop"},
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0":                                                           {"Firefox", "Windows", "desktop"},
	"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0":                                                             {"Firefox", "Linux", "desktop"},
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:133.0) Gecko/20100101 Firefox/133.0":                                                       {"Firefox", "macOS", "desktop"},
	"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/133.0 Mobile/15E148 Safari/605.1.15":  {"Firefox", "iOS", "mobile"},
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15":                      {"Safari", "macOS", "desktop"},
	"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1":    {"Safari", "iOS", "mobile"},
	"Mozilla/5.0 (iPad; CPU OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1":             {"Safari", "iOS", "tablet"},
	"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/27.0 Chrome/125.0.0.0 Mobile Safari/537.36": {"Samsung Internet", "Android", "mobile"},
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 OPR/115.0.0.0":              {"Opera", "Windows", "desktop"},

	// Crawlers and bots.
	"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":  {"Googlebot", "", "bot"},
	"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)":   {"bingbot", "", "bot"},
	"Mozilla/5.0 (compatible; Applebot/0.1; +http://www.apple.com/go/applebot)": {"Applebot", "", "bot"},
	"Mozilla/5.0 (compatible; LinkedInBot/1.0; +http://www.linkedin.com)":       {"LinkedInBot", "", "bot"},
	"Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)":                {"Slackbot", "", "bot"},
	"Twitterbot/1.0": {"Twitterbot", "", "bot"},
	"facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)": {"facebookexternalhit", "", "bot"},
	"WhatsApp/2.23.20.0":                      {"WhatsApp", "", "bot"},
	"Discordbot/2.0; +https://discordapp.com": {"Discordbot", "", "bot"},
	"Googlebot-Image/1.0":                     {"Googlebot-Image", "", "bot"},

	// HTTP clients and libraries.
	"curl/8.4.0":             {"curl", "", "tool"},
	"Wget/1.21.4":            {"Wget", "", "tool"},
	"Go-http-client/2.0":     {"Go-http-client", "", "tool"},
	"python-requests/2.32.3": {"python-requests", "", "tool"},
	"okhttp/4.12.0":          {"okhttp", "", "tool"},
	"PostmanRuntime/7.39.0":  {"PostmanRuntime", "", "tool"},
	"node-fetch/1.0 (+https://github.com/bitinn/node-fetch)": {"node-fetch", "", "tool"},
	"undici/6.19.0":    {"undici", "", "tool"},
	"axios/1.7.0":      {"axios", "", "tool"},
	"Java/17.0.10":     {"Java", "", "tool"},
	"libwww-perl/6.76": {"libwww-perl", "", "tool"},
}

// parseUserAgent names one user agent: the table first, then the token rules.
func parseUserAgent(ua string) map[string]any {
	out := map[string]any{"raw": ua}
	parsed, ok := userAgentTable[ua]
	if !ok {
		parsed = classify(ua)
	}
	if parsed.browser != "" {
		out["browser"] = parsed.browser
	}
	if parsed.os != "" {
		out["os"] = parsed.os
	}
	if parsed.device != "" {
		out["device"] = parsed.device
	}
	return out
}

// botNames are the crawlers and link unfurlers a team filters on. They are checked first,
// because several of their user agents also carry a browser token for compatibility.
var botNames = []string{
	"Googlebot", "Googlebot-Image", "bingbot", "Applebot", "LinkedInBot",
	"Slackbot", "Twitterbot", "facebookexternalhit", "WhatsApp", "Discordbot",
}

// toolNames are the HTTP clients and libraries that are neither a browser nor a crawler.
var toolNames = []string{
	"curl/", "Wget/", "Go-http-client", "python-requests", "okhttp", "PostmanRuntime",
	"node-fetch", "undici", "axios", "Java/", "libwww-perl", "HTTPie", "faraday",
}

// classify reads a user agent string with token rules, for a client the table does not hold.
// A token that matches nothing leaves its field empty, so an unknown client keeps only raw.
func classify(ua string) userAgent {
	for _, bot := range botNames {
		if strings.Contains(ua, bot) {
			return userAgent{browser: bot, device: "bot"}
		}
	}
	for _, tool := range toolNames {
		if strings.Contains(ua, tool) {
			return userAgent{browser: strings.TrimSuffix(tool, "/"), device: "tool"}
		}
	}
	return userAgent{browser: browserOf(ua), os: osOf(ua), device: deviceOf(ua)}
}

// browserOf names the browser behind a user agent, or "" when no token matches.
func browserOf(ua string) string {
	switch {
	case strings.Contains(ua, "SamsungBrowser/"):
		return "Samsung Internet"
	case strings.Contains(ua, "OPR/") || strings.Contains(ua, "Opera/"):
		return "Opera"
	case strings.Contains(ua, "Edg/") || strings.Contains(ua, "Edge/"):
		return "Edge"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		return "Chrome"
	case strings.Contains(ua, "Firefox/") || strings.Contains(ua, "FxiOS/"):
		return "Firefox"
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		return "Safari"
	default:
		return ""
	}
}

// osOf names the operating system behind a user agent, or "" when no token matches.
func osOf(ua string) string {
	switch {
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"), strings.Contains(ua, "iOS"):
		return "iOS"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		return "macOS"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	default:
		return ""
	}
}

// deviceOf names the device kind behind a user agent, or "" when no token matches.
func deviceOf(ua string) string {
	switch {
	case strings.Contains(ua, "iPad"), strings.Contains(ua, "Tablet"):
		return "tablet"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "Mobile"):
		return "mobile"
	case strings.Contains(ua, "Windows"), strings.Contains(ua, "Macintosh"),
		strings.Contains(ua, "Linux"), strings.Contains(ua, "X11"):
		return "desktop"
	default:
		return ""
	}
}
