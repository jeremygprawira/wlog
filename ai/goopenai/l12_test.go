package wlogopenai_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogopenai "github.com/jeremygprawira/wlog/ai/goopenai"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestGoOpenAI_L12_RequestIDsAppend proves three attempts keep every request id.
func TestGoOpenAI_L12_RequestIDsAppend(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	next := doerFunc(func(req *http.Request) (*http.Response, error) {
		id := req.Header.Get("X-Test-N")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"X-Request-Id": []string{"req_" + id}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	doer := wlogopenai.Doer(next)
	for i := 1; i <= 3; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("X-Test-N", fmt.Sprint(i))
		if _, err := doer.Do(req); err != nil {
			t.Fatalf("Do: %v", err)
		}
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	ids, _ := group["request_ids"].([]any)
	if fmt.Sprint(ids) != "[req_1 req_2 req_3]" {
		t.Fatalf("request_ids = %v, want [req_1 req_2 req_3]", ids)
	}
}
