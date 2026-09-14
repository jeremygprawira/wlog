package enrich

import (
	"context"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// UserAgent parses http.user_agent (set by http-std) into
// http.user_agent_parsed.{raw,browser,os,device}. An unrecognized string keeps only
// raw. There is nothing to overwrite — this field doesn't exist until UserAgent sets
// it — so it always runs, with no Option.
func UserAgent() wlog.Enricher {
	return wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
		httpField, ok := event["http"].(map[string]any)
		if !ok {
			return
		}
		ua, _ := httpField["user_agent"].(string)
		if ua == "" {
			return
		}
		httpField["user_agent_parsed"] = map[string]any{
			"raw":     ua,
			"browser": browserOf(ua),
			"os":      osOf(ua),
			"device":  deviceOf(ua),
		}
	})
}

// botNames are common HTTP clients and crawlers, checked before browser tokens since
// several bots' UA strings also contain "Safari" or similar for compatibility.
var botNames = []string{"Googlebot", "bingbot", "Slackbot", "Twitterbot", "facebookexternalhit"}

func browserOf(ua string) string {
	for _, bot := range botNames {
		if strings.Contains(ua, bot) {
			return bot
		}
	}
	switch {
	case strings.HasPrefix(ua, "curl/"):
		return "curl"
	case strings.HasPrefix(ua, "Go-http-client"):
		return "Go-http-client"
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

func deviceOf(ua string) string {
	for _, bot := range botNames {
		if strings.Contains(ua, bot) {
			return "bot"
		}
	}
	switch {
	case strings.HasPrefix(ua, "curl/"), strings.HasPrefix(ua, "Go-http-client"):
		return "bot"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"), strings.Contains(ua, "Mobile"):
		return "mobile"
	default:
		return "desktop"
	}
}
