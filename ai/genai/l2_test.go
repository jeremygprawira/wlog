package wlogenai_test

import (
	"testing"

	"google.golang.org/genai"

	wlogenai "github.com/jeremygprawira/wlog/ai/genai"
)

// TestGenai_L2_StopsWhenTheCallerStops proves a caller that breaks the range stops the stream.
func TestGenai_L2_StopsWhenTheCallerStops(t *testing.T) {
	done := make(chan struct{})
	seq := func(yield func(*genai.GenerateContentResponse, error) bool) {
		defer close(done)
		if !yield(&genai.GenerateContentResponse{ResponseID: "resp_01"}, nil) {
			return
		}
		yield(&genai.GenerateContentResponse{ResponseID: "resp_02"}, nil)
	}
	out, _ := wlogenai.Observe(seq, genai.BackendGeminiAPI)
	for range out {
		break
	}
	select {
	case <-done:
	default:
		t.Fatal("the stream kept running after the caller stopped")
	}
}
