package wlogopenai_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestOpenAI_L12_RequestIDsAppend proves three attempts keep every request id.
func TestOpenAI_L12_RequestIDsAppend(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	mw := wlogopenai.Middleware()
	for i := 1; i <= 3; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp := &http.Response{
			Header: http.Header{"X-Request-Id": []string{fmt.Sprintf("req_%d", i)}},
			Body:   io.NopCloser(strings.NewReader("")),
		}
		next := func(*http.Request) (*http.Response, error) { return resp, nil }
		if _, err := mw(req, next); err != nil {
			t.Fatalf("middleware: %v", err)
		}
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	ids, _ := group["request_ids"].([]any)
	if fmt.Sprint(ids) != "[req_1 req_2 req_3]" {
		t.Fatalf("request_ids = %v, want [req_1 req_2 req_3]", ids)
	}
}
