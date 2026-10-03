package llm_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLLM_L1_AddDoesNotRaceSetGroup proves two model calls can update one event together.
func TestLLM_L1_AddDoesNotRaceSetGroup(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "chat")

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			llm.Add(ctx, llm.Record{Model: "claude-sonnet-4-6", InputTokens: 1})
		}()
		go func() {
			defer wg.Done()
			wlog.SetGroup(ctx, "llm", "operation", "chat")
		}()
	}
	wg.Wait()
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if fmt.Sprint(group["input_tokens"]) != "40" {
		t.Fatalf("input_tokens = %v, want 40", group["input_tokens"])
	}
}
