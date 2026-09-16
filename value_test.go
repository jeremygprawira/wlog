// This file proves the ownership and conversion rules of the value tree.
//
// Each test is named for the finding it closes, so a later change that reopens a
// finding fails a test whose name says which rule it broke.
package wlog_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestCore_CORE4_CallerMapUnchanged proves that a map handed to Set is copied,
// so a later change by the caller never reaches the event, and the copy runs
// before the lock, so a MarshalJSON that logs finishes.
func TestCore_CORE4_CallerMapUnchanged(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	meta := map[string]any{"plan": "pro", "seats": 3}
	ctx, end := wlog.Start(ctxBase, "checkout")
	wlog.Set(ctx, "metadata", meta)
	meta["plan"] = "free"
	meta["seats"] = 99
	end()

	got := rec.Last()["metadata"].(map[string]any)
	if got["plan"] != "pro" || got["seats"] != int64(3) {
		t.Errorf("metadata = %v, want the value at write time", got)
	}
}

// loggingValue logs through wlog while the value is copied.
type loggingValue struct {
	ctx  context.Context
	logs *int
}

// MarshalJSON writes a field on the same event, which deadlocks when the event
// lock is already held.
func (v loggingValue) MarshalJSON() ([]byte, error) {
	wlog.Set(v.ctx, "from_marshal", "ok")
	*v.logs++
	return []byte(`"value"`), nil
}

// TestCore_CORE4_MarshalJSONLogsOnSameEvent proves that a value which logs
// through wlog while it is copied finishes, because the copy runs before the
// event lock is taken.
func TestCore_CORE4_MarshalJSONLogsOnSameEvent(t *testing.T) {
	log, _ := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	logs := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, end := wlog.Start(ctxBase, "marshal")
		wlog.Set(ctx, "value", loggingValue{ctx: ctx, logs: &logs})
		end()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Set deadlocked: the copy ran while the event lock was held")
	}
	if logs != 1 {
		t.Errorf("MarshalJSON ran %d times, want 1", logs)
	}
}

// TestCore_CORE6_Int64Exact proves that an integer keeps every digit, so a large
// identifier never loses precision.
func TestCore_CORE6_Int64Exact(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctxBase, "ids")
	wlog.Set(ctx, "int64", int64(math.MaxInt64))
	wlog.Set(ctx, "uint64", uint64(math.MaxUint64))
	wlog.Set(ctx, "int", int(1<<62))
	end()

	got := rec.Last()
	if got["int64"] != int64(math.MaxInt64) {
		t.Errorf("int64 = %v (%T), want the exact value", got["int64"], got["int64"])
	}
	if got["uint64"] != uint64(math.MaxUint64) {
		t.Errorf("uint64 = %v (%T), want the exact value", got["uint64"], got["uint64"])
	}
	if got["int"] != int64(1<<62) {
		t.Errorf("int = %v (%T), want an int64", got["int"], got["int"])
	}
}

// TestCore_CORE5_NaNIsString proves that the three floats JSON cannot hold
// become their names.
func TestCore_CORE5_NaNIsString(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctxBase, "floats")
	wlog.Set(ctx, "nan", math.NaN())
	wlog.Set(ctx, "pos", math.Inf(1))
	wlog.Set(ctx, "neg", math.Inf(-1))
	wlog.Set(ctx, "ok", 1.5)
	end()

	got := rec.Last()
	for key, want := range map[string]string{"nan": "NaN", "pos": "+Inf", "neg": "-Inf"} {
		if got[key] != want {
			t.Errorf("%s = %v (%T), want %q", key, got[key], got[key], want)
		}
	}
	if got["ok"] != 1.5 {
		t.Errorf("ok = %v, want 1.5", got["ok"])
	}
}

// TestCore_CORE3_JSONDashNeverLeaks proves that a field tagged json:"-" never
// reaches the event, on any path.
func TestCore_CORE3_JSONDashNeverLeaks(t *testing.T) {
	type account struct {
		ID     int    `json:"id"`
		Secret string `json:"-"`
		Token  string `json:"token,omitempty"`
		hidden string //nolint:unused // unexported on purpose: JSON drops it
	}

	log, rec := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctxBase, "account")
	wlog.Set(ctx, "account", account{ID: 7, Secret: "SHOULD-NOT-APPEAR", hidden: "also-secret"})
	end()

	line, err := json.Marshal(rec.Last())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(line), "SHOULD-NOT-APPEAR") || strings.Contains(string(line), "also-secret") {
		t.Errorf("a json:\"-\" field reached the event: %s", line)
	}
	got := rec.Last()["account"].(map[string]any)
	if got["id"] != int64(7) {
		t.Errorf("account.id = %v, want 7", got["id"])
	}
	if _, ok := got["token"]; ok {
		t.Errorf("omitempty kept an empty field: %v", got)
	}
}

// TestCore_CORE26_ErrorAndDuration proves the conversions of an error, a
// duration, and a time, and that a panicking Error never reaches the caller.
func TestCore_CORE26_ErrorAndDuration(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctxBase, "convert")
	wlog.Set(ctx, "err", errors.New("boom"))
	wlog.Set(ctx, "duration", 1500*time.Millisecond)
	wlog.Set(ctx, "time", time.Date(2026, 9, 16, 12, 0, 0, 0, time.FixedZone("x", 3600)))
	wlog.Set(ctx, "raw", json.RawMessage(`{"a":1}`))
	wlog.Set(ctx, "bytes", []byte{1, 2, 3})
	wlog.Set(ctx, "panic", panicError{})
	end()

	got := rec.Last()
	if got["err"] != "boom" {
		t.Errorf("err = %v (%T), want the message", got["err"], got["err"])
	}
	if got["duration"] != 1500.0 {
		t.Errorf("duration = %v (%T), want 1500", got["duration"], got["duration"])
	}
	if got["time"] != "2026-09-16T11:00:00Z" {
		t.Errorf("time = %v, want RFC 3339 UTC", got["time"])
	}
	if raw, ok := got["raw"].(map[string]any); !ok || raw["a"] != int64(1) {
		t.Errorf("raw = %v (%T), want the decoded object", got["raw"], got["raw"])
	}
	if got["bytes"] != "[binary: 3 bytes]" {
		t.Errorf("bytes = %v, want the byte count", got["bytes"])
	}
	if got["panic"] != "[error: panicError panicked]" {
		t.Errorf("panic = %v, want the typed panic text", got["panic"])
	}
}

// panicError panics in Error, which a logging call must survive.
type panicError struct{}

// Error panics on purpose.
func (panicError) Error() string { panic("boom") }

// TestCore_CORE2_NestedMapStringStringRedacted proves that a typed map and a
// nested struct are copied, and that the redactor still sees them.
func TestCore_CORE2_NestedMapStringStringRedacted(t *testing.T) {
	type inner struct {
		Password string `json:"password"`
		Plan     string `json:"plan"`
	}
	type outer struct {
		Inner  inner             `json:"inner"`
		Labels map[string]string `json:"labels"`
	}

	log, rec := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctxBase, "nested")
	wlog.Set(ctx, "data", outer{
		Inner:  inner{Password: "hunter2", Plan: "pro"},
		Labels: map[string]string{"password": "also-secret", "tier": "gold"},
	})
	end()

	line, err := json.Marshal(rec.Last())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(line), "hunter2") || strings.Contains(string(line), "also-secret") {
		t.Errorf("a nested password reached the drain: %s", line)
	}
	got := rec.Last()["data"].(map[string]any)
	if got["labels"].(map[string]any)["tier"] != "gold" {
		t.Errorf("labels = %v, want the map copied", got["labels"])
	}
	if got["inner"].(map[string]any)["plan"] != "pro" {
		t.Errorf("inner = %v, want the struct copied by its json tags", got["inner"])
	}
}

// TestCore_UnencodableAndDepth proves the two fallbacks: a value JSON cannot
// hold, and nesting past the depth cap.
func TestCore_UnencodableAndDepth(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	deep := any("leaf")
	for i := 0; i < 20; i++ {
		deep = map[string]any{"next": deep}
	}

	ctx, end := wlog.Start(ctxBase, "fallbacks")
	wlog.Set(ctx, "func", func() {})
	wlog.Set(ctx, "deep", deep)
	end()

	got := rec.Last()
	if !strings.HasPrefix(got["func"].(string), "[unencodable: ") {
		t.Errorf("func = %v, want the unencodable text", got["func"])
	}
	text, err := json.Marshal(got["deep"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "[truncated: depth]") {
		t.Errorf("deep = %s, want the depth fallback", text)
	}
}

// TestCore_StringOptionAndEmbedded proves the two remaining json tags: the
// ",string" option and an embedded struct.
func TestCore_StringOptionAndEmbedded(t *testing.T) {
	type base struct {
		ID int64 `json:"id"`
	}
	type typed struct {
		base
		Count int  `json:"count,string"`
		Flag  bool `json:"flag,string"`
	}

	log, rec := wlogtest.New(t)
	ctxBase := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctxBase, "tags")
	wlog.Set(ctx, "v", typed{base: base{ID: 5}, Count: 12, Flag: true})
	end()

	got := rec.Last()["v"].(map[string]any)
	if got["count"] != "12" || got["flag"] != "true" {
		t.Errorf("the ,string option did not apply: %v", got)
	}
	if _, ok := got["id"]; !ok {
		t.Errorf("the embedded struct did not flatten: %v", got)
	}
}

// TestCore_Copy_EdgeShapes proves the remaining copy paths: a map with a key that
// is not a string, a big JSON number, and a nil pointer.
func TestCore_Copy_EdgeShapes(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "shapes")
	wlog.Set(ctx, "int_keys", map[int]string{1: "one"})
	wlog.Set(ctx, "big", json.Number("123456789012345678901234567890"))
	wlog.Set(ctx, "nil_ptr", (*struct{ A int })(nil))
	end()

	got := rec.Last()
	if text, ok := got["int_keys"].(string); !ok || !strings.HasPrefix(text, "[unencodable: ") {
		t.Errorf("int_keys = %v, want the unencodable text: JSON needs string keys", got["int_keys"])
	}
	if fmt.Sprint(got["big"]) != "123456789012345678901234567890" {
		t.Errorf("big = %v (%T), want every digit kept", got["big"], got["big"])
	}
	if got["nil_ptr"] != nil {
		t.Errorf("nil_ptr = %v, want nil", got["nil_ptr"])
	}
}

// TestCore_Copy_EveryShape proves the remaining copy paths: typed slices, arrays,
// a typed map with a string key, the string option on every scalar kind, and the
// public extractor.
func TestCore_Copy_EveryShape(t *testing.T) {
	type tags struct {
		Strs  []string       `json:"strs"`
		Ints  []int          `json:"ints"`
		Arr   [2]int         `json:"arr"`
		Map   map[string]int `json:"map"`
		Bytes []byte         `json:"bytes"`
		Low   int            `json:"low,string"`
		High  uint8          `json:"high,string"`
		F     float32        `json:"f,string"`
		B     bool           `json:"b,string"`
	}

	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "shapes")
	wlog.Set(ctx, "v", tags{
		Strs: []string{"a"}, Ints: []int{1}, Arr: [2]int{1, 2}, Map: map[string]int{"k": 3},
		Bytes: []byte{1, 2}, Low: 7, High: 8, F: 1.5, B: true,
	})
	end()

	got := rec.Last()["v"].(map[string]any)
	if got["strs"].([]any)[0] != "a" || got["ints"].([]any)[0] != int64(1) {
		t.Errorf("slices = %v / %v", got["strs"], got["ints"])
	}
	if len(got["arr"].([]any)) != 2 || got["map"].(map[string]any)["k"] != int64(3) {
		t.Errorf("array or map = %v / %v", got["arr"], got["map"])
	}
	if got["bytes"] != "[binary: 2 bytes]" {
		t.Errorf("bytes = %v, want the byte count", got["bytes"])
	}
	for key, want := range map[string]string{"low": "7", "high": "8", "f": "1.5", "b": "true"} {
		if got[key] != want {
			t.Errorf("%s = %v (%T), want the string %q", key, got[key], got[key], want)
		}
	}

	if _, ok := wlog.DefaultExtractor().Extract(errors.New("boom")).Code, true; !ok {
		t.Error("DefaultExtractor returned no code")
	}
	if _, ok := wlog.Field(ctx, "v"); !ok {
		t.Error("Field did not find a field that Set stored")
	}
}
