package wlogopenai_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestOpenAI_L11_JSONArgumentKeyIsMasked proves a denied key inside a tool
// argument JSON string is masked, and the rest of the argument stays.
func TestOpenAI_L11_JSONArgumentKeyIsMasked(t *testing.T) {
	chat := chatCompletion(t)
	chat.Choices[0].Message.ToolCalls[0].Function.Arguments = `{"password":"hunter2","city":"Jakarta"}`
	record := wlogopenai.FromChatCompletion(chat, wlogopenai.WithContent())

	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "chat")
	llm.Add(ctx, record)
	end()

	body, err := json.Marshal(rec.Last())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(body)
	if strings.Contains(text, "hunter2") {
		t.Fatalf("password value reached the event: %s", text)
	}
	if !strings.Contains(text, "Jakarta") {
		t.Fatalf("city was dropped with the password: %s", text)
	}
}
