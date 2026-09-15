package wlog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
)

func startEvent(t *testing.T) (context.Context, func() map[string]any) {
	t.Helper()
	log := wlog.New()
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "test.op")

	return ctx, func() map[string]any {
		out := captureStdout(t, end)
		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("invalid JSON line: %v\noutput: %q", err, out)
		}
		return got
	}
}

func TestCore_SetGroup_MergesIntoOneSlot(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.SetGroup(ctx, "payment", "method", "va", "amount", 150000)
	wlog.SetGroup(ctx, "payment", "status", "settled")
	got := finish()

	payment, ok := got["payment"].(map[string]any)
	if !ok {
		t.Fatalf("payment group missing or wrong type: %v", got["payment"])
	}
	if payment["method"] != "va" || payment["status"] != "settled" {
		t.Errorf("payment group = %v", payment)
	}
}

func TestCore_SetGroup_AcceptsMap(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.SetGroup(ctx, "form", map[string]any{"code": "F1", "count": float64(3)})
	got := finish()

	form := got["form"].(map[string]any)
	if form["code"] != "F1" || form["count"] != float64(3) {
		t.Errorf("form group = %v", form)
	}
}

func TestCore_Append_BuildsArray(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.Append(ctx, "steps", "validate")
	wlog.Append(ctx, "steps", "charge")
	got := finish()

	steps, ok := got["steps"].([]any)
	if !ok || len(steps) != 2 || steps[0] != "validate" || steps[1] != "charge" {
		t.Errorf("steps = %v", got["steps"])
	}
}

type benchUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestCore_Set_NormalizesStructsViaJSONTags(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.Set(ctx, "user", benchUser{ID: 42, Name: "alice"})
	got := finish()

	user, ok := got["user"].(map[string]any)
	if !ok || user["id"] != float64(42) || user["name"] != "alice" {
		t.Errorf("user = %v", got["user"])
	}
}

func TestCore_Set_KeyCap(t *testing.T) {
	ctx, finish := startEvent(t)
	for i := 0; i < 205; i++ {
		wlog.Set(ctx, fmt.Sprintf("k%d", i), i)
	}
	got := finish()

	count := 0
	for k := range got {
		if len(k) > 1 && k[0] == 'k' {
			count++
		}
	}
	if count != 200 {
		t.Errorf("kept %d keys, want 200 (the cap)", count)
	}
	if got["wlog.dropped_fields"] != float64(5) {
		t.Errorf("wlog.dropped_fields = %v, want 5", got["wlog.dropped_fields"])
	}
}

func TestCore_SetGroup_FieldCap(t *testing.T) {
	ctx, finish := startEvent(t)
	for i := 0; i < 55; i++ {
		wlog.SetGroup(ctx, "g", fmt.Sprintf("f%d", i), i)
	}
	got := finish()

	g := got["g"].(map[string]any)
	if len(g) != 50 {
		t.Errorf("group has %d fields, want 50 (the cap)", len(g))
	}
	if got["wlog.dropped_fields"] != float64(5) {
		t.Errorf("wlog.dropped_fields = %v, want 5", got["wlog.dropped_fields"])
	}
}

func TestCore_Append_LenCap(t *testing.T) {
	ctx, finish := startEvent(t)
	for i := 0; i < 205; i++ {
		wlog.Append(ctx, "arr", i)
	}
	got := finish()

	arr := got["arr"].([]any)
	if len(arr) != 200 {
		t.Errorf("array has %d elements, want 200 (the cap)", len(arr))
	}
	if got["wlog.dropped_fields"] != float64(5) {
		t.Errorf("wlog.dropped_fields = %v, want 5", got["wlog.dropped_fields"])
	}
}

func TestCore_ConcurrentWriters(t *testing.T) {
	ctx, finish := startEvent(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			wlog.Set(ctx, fmt.Sprintf("g%d", i), i)
			wlog.SetGroup(ctx, "shared", fmt.Sprintf("f%d", i), i)
			wlog.Append(ctx, "list", i)
		}(i)
	}
	wg.Wait()
	got := finish()

	shared := got["shared"].(map[string]any)
	if len(shared) != 32 {
		t.Errorf("shared group has %d fields, want 32", len(shared))
	}
	list := got["list"].([]any)
	if len(list) != 32 {
		t.Errorf("list has %d elements, want 32", len(list))
	}
}

func TestCore_HasEvent(t *testing.T) {
	log := wlog.New()
	ctx := log.WithContext(context.Background())

	if wlog.HasEvent(ctx) {
		t.Error("HasEvent = true before Start, want false")
	}

	ctx, end := wlog.Start(ctx, "test.op")
	if !wlog.HasEvent(ctx) {
		t.Error("HasEvent = false inside a started event, want true")
	}
	captureStdout(t, end)

	if wlog.HasEvent(context.Background()) {
		t.Error("HasEvent = true on a bare context, want false")
	}
}
