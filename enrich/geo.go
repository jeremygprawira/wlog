package enrich

import (
	"context"
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

// GeoHeaders uses a caller-supplied header map (canonical field -> header name)
// instead of the built-in Cloudflare/CloudFront/Vercel detection.
func GeoHeaders(headers map[string]string) Option {
	return func(c *config) { c.geoHeaders = headers }
}

// Geo adds geo.country/region/city/lat/lon read from CDN headers on
// http.request_headers (set by http-std): CloudFront or Vercel (full detail) or
// Cloudflare (country only), tried in that order, or a custom set via GeoHeaders.
func Geo(opts ...Option) wlog.Enricher {
	cfg := newConfig(opts)
	return wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
		headers := requestHeaders(event)
		if headers == nil {
			return
		}

		var fields map[string]any
		if cfg.geoHeaders != nil {
			fields = extractGeo(headers, cfg.geoHeaders)
		} else {
			for _, set := range []geoHeaderSet{cloudFrontHeaders, vercelHeaders, cloudflareHeaders} {
				if f := extractGeo(headers, set); len(f) > 0 {
					fields = f
					break
				}
			}
		}
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

func extractGeo(headers map[string]any, set geoHeaderSet) map[string]any {
	fields := map[string]any{}
	for canonical, headerName := range set {
		if v, ok := headerLookup(headers, headerName); ok && v != "" {
			fields[canonical] = v
		}
	}
	return fields
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
