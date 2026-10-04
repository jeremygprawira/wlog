package wlogenai_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogenai "github.com/jeremygprawira/wlog/ai/genai"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestGenai_L13_AttemptsArePerCall proves three calls with no retry do not add up.
func TestGenai_L13_AttemptsArePerCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(""))}
	next := roundTripFunc(func(*http.Request) (*http.Response, error) { return resp, nil })
	rt := wlogenai.Transport(next)
	for range 3 {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		if _, err := rt.RoundTrip(req); err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if group["attempts"] != int64(1) {
		t.Fatalf("attempts = %v, want 1 for each call", group["attempts"])
	}
}
