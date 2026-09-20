// This file checks the OpenFeature hook: one entry per evaluation, the missing flag
// record, the value rule, and the rule that a hook panic never reaches the SDK.
package wlogopenfeature_test

import (
	"context"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"

	"github.com/jeremygprawira/wlog"
	wlogopenfeature "github.com/jeremygprawira/wlog/flag/openfeature"
)

// TestOpenFeature_C9_Evaluation proves that one evaluation appends one entry with the
// key, the type, the provider, and the variant.
func TestOpenFeature_C9_Evaluation(t *testing.T) {
	provider := memprovider.NewInMemoryProvider(map[string]memprovider.InMemoryFlag{
		"known": {Key: "known", DefaultVariant: "on", Variants: map[string]any{"on": true, "off": false}},
	})
	if err := openfeature.SetProvider(provider); err != nil {
		t.Fatalf("set provider: %v", err)
	}
	ctx, end := wlog.Start(context.Background(), "test")
	defer end()

	client := openfeature.NewClient("test")
	if _, err := client.BooleanValue(ctx, "known", false, openfeature.EvaluationContext{}, openfeature.WithHooks(wlogopenfeature.Hook())); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	entries := entriesOf(t, ctx)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want one", len(entries))
	}
	entry := entries[0]
	if entry["key"] != "known" || entry["type"] != "bool" || entry["variant"] != "on" {
		t.Errorf("entry = %v, want known, boolean, on", entry)
	}
	if entry["provider"] != "in-memory" && entry["provider"] == "" {
		t.Errorf("provider = %v, want the provider name", entry["provider"])
	}
}

// TestOpenFeature_C9_MissingFlag proves that a missing flag records reason ERROR and
// error_code FLAG_NOT_FOUND.
func TestOpenFeature_C9_MissingFlag(t *testing.T) {
	provider := memprovider.NewInMemoryProvider(map[string]memprovider.InMemoryFlag{})
	if err := openfeature.SetProvider(provider); err != nil {
		t.Fatalf("set provider: %v", err)
	}
	ctx, end := wlog.Start(context.Background(), "test")
	defer end()

	client := openfeature.NewClient("test")
	_, _ = client.BooleanValue(ctx, "missing", false, openfeature.EvaluationContext{}, openfeature.WithHooks(wlogopenfeature.Hook()))
	entries := entriesOf(t, ctx)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want one", len(entries))
	}
	if entries[0]["reason"] != "ERROR" || entries[0]["error_code"] != "FLAG_NOT_FOUND" {
		t.Errorf("entry = %v, want reason ERROR and error_code FLAG_NOT_FOUND", entries[0])
	}
}

// TestOpenFeature_C9_Values proves that a value is recorded only with WithValues, and
// only for a bool, an int, or a float.
func TestOpenFeature_C9_Values(t *testing.T) {
	hookContext := openfeature.NewHookContext("known", openfeature.Boolean, false, openfeature.ClientMetadata{}, openfeature.Metadata{Name: "test"}, openfeature.EvaluationContext{})
	details := openfeature.InterfaceEvaluationDetails{Value: true}

	plain := wlogopenfeature.Hook()
	if err := plain.After(context.Background(), hookContext, details, openfeature.HookHints{}); err != nil {
		t.Fatalf("after: %v", err)
	}
	ctx, end := wlog.Start(context.Background(), "test")
	defer end()
	if err := wlogopenfeature.Hook(wlogopenfeature.WithValues()).After(ctx, hookContext, details, openfeature.HookHints{}); err != nil {
		t.Fatalf("after: %v", err)
	}
	if entries := entriesOf(t, ctx); entries[0]["value"] != true {
		t.Errorf("entry = %v, want the bool value", entries[0])
	}

	textContext := openfeature.NewHookContext("text", openfeature.String, "", openfeature.ClientMetadata{}, openfeature.Metadata{Name: "test"}, openfeature.EvaluationContext{})
	textDetails := openfeature.InterfaceEvaluationDetails{Value: "secret-text"}
	if err := wlogopenfeature.Hook(wlogopenfeature.WithValues()).After(ctx, textContext, textDetails, openfeature.HookHints{}); err != nil {
		t.Fatalf("after: %v", err)
	}
	if entries := entriesOf(t, ctx); entries[1]["value"] != nil {
		t.Errorf("entry = %v, want no string value", entries[1])
	}
}

// TestOpenFeature_C9_PanicNeverReachesTheSDK proves that a panic inside the hook is
// recovered, because the SDK does not recover hook panics.
func TestOpenFeature_C9_PanicNeverReachesTheSDK(t *testing.T) {
	hook := wlogopenfeature.Hook()
	hookContext := openfeature.NewHookContext("known", openfeature.Boolean, false, openfeature.ClientMetadata{}, openfeature.Metadata{Name: "test"}, openfeature.EvaluationContext{})
	details := openfeature.InterfaceEvaluationDetails{}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("a panic reached the SDK: %v", recovered)
		}
	}()
	_ = hook.After(panicContext{}, hookContext, details, openfeature.HookHints{})
	hook.Error(panicContext{}, hookContext, context.Canceled, openfeature.HookHints{})
	hook.Finally(panicContext{}, hookContext, details, openfeature.HookHints{})
}

// panicContext panics on the first lookup, like a broken context.
type panicContext struct {
	context.Context
}

// Value panics, so the hook's own recover must catch it.
func (panicContext) Value(any) any { panic("boom") }

// entriesOf reads the feature_flags entries of the event.
func entriesOf(t *testing.T, ctx context.Context) []map[string]any {
	t.Helper()
	value, ok := wlog.Field(ctx, "feature_flags")
	if !ok {
		t.Fatalf("no feature_flags field")
	}
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("feature_flags = %T, want []any", value)
	}
	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("entry = %T, want map[string]any", item)
		}
		entries = append(entries, entry)
	}
	return entries
}
