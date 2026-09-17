package enrich_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog/enrich"
)

func headersEvent(headers map[string]any) map[string]any {
	return map[string]any{"http": map[string]any{"request_headers": headers}}
}

func TestGeo_CloudFront_FullDetail(t *testing.T) {
	event := headersEvent(map[string]any{
		"CloudFront-Viewer-Country":        "ID",
		"CloudFront-Viewer-Country-Region": "JK",
		"CloudFront-Viewer-City":           "Jakarta",
		"CloudFront-Viewer-Latitude":       "-6.2",
		"CloudFront-Viewer-Longitude":      "106.8",
	})
	enrich.Geo("cloudfront").Enrich(context.Background(), event)

	geo := event["geo"].(map[string]any)
	if geo["country"] != "ID" || geo["region"] != "JK" || geo["city"] != "Jakarta" ||
		geo["lat"] != -6.2 || geo["lon"] != 106.8 {
		t.Errorf("geo = %v", geo)
	}
}

func TestGeo_Vercel_FullDetail(t *testing.T) {
	event := headersEvent(map[string]any{
		"X-Vercel-IP-Country":        "SG",
		"X-Vercel-IP-Country-Region": "01",
		"X-Vercel-IP-City":           "Singapore",
	})
	enrich.Geo("vercel").Enrich(context.Background(), event)

	geo := event["geo"].(map[string]any)
	if geo["country"] != "SG" || geo["city"] != "Singapore" {
		t.Errorf("geo = %v", geo)
	}
}

func TestGeo_Cloudflare_CountryOnly(t *testing.T) {
	event := headersEvent(map[string]any{"CF-IPCountry": "US"})
	enrich.Geo("cloudflare").Enrich(context.Background(), event)

	geo := event["geo"].(map[string]any)
	if geo["country"] != "US" {
		t.Errorf("geo.country = %v, want US", geo["country"])
	}
	if _, ok := geo["city"]; ok {
		t.Error("Cloudflare's single header should not produce a city")
	}
}

func TestGeo_CustomHeaders(t *testing.T) {
	event := headersEvent(map[string]any{"X-My-Country": "MY"})
	enrich.Geo("custom", enrich.GeoHeaders(map[string]string{"country": "X-My-Country"})).
		Enrich(context.Background(), event)

	geo := event["geo"].(map[string]any)
	if geo["country"] != "MY" {
		t.Errorf("geo.country = %v, want MY", geo["country"])
	}
}

func TestGeo_NoHeaders_IsNoop(t *testing.T) {
	event := headersEvent(map[string]any{})
	enrich.Geo("cloudfront").Enrich(context.Background(), event)
	if _, ok := event["geo"]; ok {
		t.Error("geo field created with no matching headers")
	}
}

func TestUser_SetsID(t *testing.T) {
	event := map[string]any{}
	enrich.User(func(context.Context) string { return "u_42" }).Enrich(context.Background(), event)

	user := event["user"].(map[string]any)
	if user["id"] != "u_42" {
		t.Errorf("user.id = %v, want u_42", user["id"])
	}
}

func TestUser_EmptyID_IsNoop(t *testing.T) {
	event := map[string]any{}
	enrich.User(func(context.Context) string { return "" }).Enrich(context.Background(), event)
	if _, ok := event["user"]; ok {
		t.Error("user field created for an empty id")
	}
}
