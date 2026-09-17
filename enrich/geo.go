package enrich

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// geoHeaderSet maps a canonical geo field (country, region, city, lat, lon) to the
// request header carrying it.
type geoHeaderSet map[string]string

var (
	cloudflareHeaders = geoHeaderSet{"country": "CF-IPCountry"}
	cloudFrontHeaders = geoHeaderSet{
		"country": "CloudFront-Viewer-Country",
		"region":  "CloudFront-Viewer-Country-Region",
		"city":    "CloudFront-Viewer-City",
		"lat":     "CloudFront-Viewer-Latitude",
		"lon":     "CloudFront-Viewer-Longitude",
	}
	vercelHeaders = geoHeaderSet{
		"country": "X-Vercel-IP-Country",
		"region":  "X-Vercel-IP-Country-Region",
		"city":    "X-Vercel-IP-City",
		"lat":     "X-Vercel-IP-Latitude",
		"lon":     "X-Vercel-IP-Longitude",
	}
)

// geoProviders maps a provider name to the headers it sets.
var geoProviders = map[string]geoHeaderSet{
	"cloudflare": cloudflareHeaders,
	"cloudfront": cloudFrontHeaders,
	"vercel":     vercelHeaders,
}

// GeoHeaders uses a caller-supplied header map (canonical field -> header name) with the
// "custom" provider.
func GeoHeaders(headers map[string]string) Option {
	return func(c *config) { c.geoHeaders = headers }
}

// Geo adds geo.country/region/city/lat/lon read from the headers of ONE named provider:
// "cloudflare", "cloudfront", "vercel", or "custom" with GeoHeaders.
//
// Read this before trusting what it records. A CDN header is only as good as the path the
// request took: a client can send CF-IPCountry, CloudFront-Viewer-City, or X-Vercel-IP-City
// itself, and this enricher would then record the client's own claim as a fact about where it
// is. Turn it on only behind the CDN that overwrites those headers, and refuse a request that
// did not come from that CDN. The older behaviour of trying several providers is gone for the
// same reason: trying Cloudflare after CloudFront means trusting a header no trusted hop set.
//
// A name the package does not know, including an empty one, trusts no provider, and the
// returned enricher adds nothing.
func Geo(provider string, opts ...Option) wlog.Enricher {
	cfg := newConfig(opts)
	name := strings.ToLower(strings.TrimSpace(provider))
	set := geoProviders[name]
	if set == nil && name == "custom" {
		set = cfg.geoHeaders
	}
	if len(set) == 0 {
		return wlog.EnricherFunc(func(context.Context, map[string]any) {})
	}
	return wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
		headers := requestHeaders(event)
		if headers == nil {
			return
		}
		fields := extractGeo(headers, set)
		if len(fields) == 0 {
			return
		}
		mergeGroup(event, "geo", fields, cfg.overwrite)
	})
}

func requestHeaders(event map[string]any) map[string]any {
	httpField, ok := event["http"].(map[string]any)
	if !ok {
		return nil
	}
	headers, _ := httpField["request_headers"].(map[string]any)
	return headers
}

// extractGeo reads one provider's headers into canonical fields.
//
// Latitude and longitude become floats, so a query can compare them and a dashboard can plot
// them, and a city is percent-decoded, which is how Vercel sends one.
func extractGeo(headers map[string]any, set geoHeaderSet) map[string]any {
	fields := map[string]any{}
	for canonical, headerName := range set {
		v, ok := headerLookup(headers, headerName)
		if !ok || v == "" {
			continue
		}
		switch canonical {
		case "lat", "lon":
			number, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				continue
			}
			fields[canonical] = number
		case "city":
			fields[canonical] = decodeCity(v)
		default:
			fields[canonical] = v
		}
	}
	return fields
}

// decodeCity percent-decodes a city name, and keeps it as written when it cannot be decoded.
func decodeCity(city string) string {
	if decoded, err := url.QueryUnescape(city); err == nil {
		return decoded
	}
	return city
}

// headerLookup matches case-insensitively, since captured headers keep whatever
// casing net/http.Header canonicalized them to, which may differ from a CDN's own
// documented header spelling.
func headerLookup(headers map[string]any, name string) (string, bool) {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			s, ok := v.(string)
			return s, ok
		}
	}
	return "", false
}
