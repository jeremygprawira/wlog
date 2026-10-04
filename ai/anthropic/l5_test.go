package wloganthropic_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wloganthropic "github.com/jeremygprawira/wlog/ai/anthropic"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAnthropic_L5_RetryCountFromRequest proves the retry count comes from the request.
func TestAnthropic_L5_RetryCountFromRequest(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Stainless-Retry-Count", "2")
	resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	next := func(*http.Request) (*http.Response, error) { return resp, nil }
	if _, err := wloganthropic.Middleware()(req, next); err != nil {
		t.Fatalf("middleware: %v", err)
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if group["attempts"] != int64(3) {
		t.Fatalf("attempts = %v, want 3 from the request header", group["attempts"])
	}
}
