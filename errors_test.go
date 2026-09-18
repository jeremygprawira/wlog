package wlog_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

func TestCore_Error_DefaultExtractor(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.Error(ctx, errors.New("order not found"))
	got := finish()

	errInfo, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("error field missing or wrong type: %v", got["error"])
	}
	if errInfo["code"] != "INTERNAL" || errInfo["message"] != "order not found" {
		t.Errorf("error = %v", errInfo)
	}
	if got["level"] != "error" || got["outcome"] != "error" {
		t.Errorf("level=%v outcome=%v, want error/error", got["level"], got["outcome"])
	}
}

func TestCore_Error_EarlierErrorsGoToErrorsList(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.Error(ctx, errors.New("first failure"))
	wlog.Error(ctx, errors.New("second failure"))
	got := finish()

	errInfo := got["error"].(map[string]any)
	if errInfo["message"] != "second failure" {
		t.Errorf("error.message = %v, want second failure (the last one reported)", errInfo["message"])
	}
	errs, ok := got["errors"].([]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("errors = %v, want one earlier entry", got["errors"])
	}
	first := errs[0].(map[string]any)
	if first["message"] != "first failure" {
		t.Errorf("errors[0].message = %v, want first failure", first["message"])
	}
}

func TestCore_Error_ListCappedAtTen(t *testing.T) {
	ctx, finish := startEvent(t)
	for i := 0; i < 12; i++ {
		wlog.Error(ctx, errors.New("failure"))
	}
	got := finish()

	errs := got["errors"].([]any)
	if len(errs) != 10 {
		t.Errorf("errors has %d entries, want 10 (the cap)", len(errs))
	}
	if got["wlog.dropped_fields"] != float64(1) {
		t.Errorf("wlog.dropped_fields = %v, want 1", got["wlog.dropped_fields"])
	}
}

// customExtractor lets tests plug in why/fix/link without a real error library.
type customExtractor struct{}

func (customExtractor) Extract(err error) wlog.ErrorInfo {
	return wlog.ErrorInfo{
		Code: "ORDER_NOT_FOUND", Kind: "not_found", Status: 404,
		Message: err.Error(), Why: "no such order id", Fix: "check the order id",
	}
}

func TestCore_Error_CustomExtractor(t *testing.T) {
	log := wlog.New(wlog.WithErrorExtractor(customExtractor{}))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "order.get")
		wlog.Error(ctx, errors.New("no row"))
		end()
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON line: %v\noutput: %q", err, out)
	}
	errInfo := got["error"].(map[string]any)
	if errInfo["code"] != "ORDER_NOT_FOUND" || errInfo["why"] != "no such order id" || errInfo["fix"] != "check the order id" {
		t.Errorf("error = %v", errInfo)
	}
}

// dataExtractor fills the audience split plus the older Attrs field.
type dataExtractor struct{}

func (dataExtractor) Extract(err error) wlog.ErrorInfo {
	return wlog.ErrorInfo{
		Code:     "VALIDATION_FAILED",
		Message:  err.Error(),
		Data:     map[string]any{"field": "email"},
		Internal: map[string]any{"row_id": "r-1"},
		Attrs:    map[string]any{"legacy": "kept"},
	}
}

// TestCore_ErrorData_ReachesEvent proves Data, Internal, and Attrs all reach the event.
func TestCore_ErrorData_ReachesEvent(t *testing.T) {
	log := wlog.New(wlog.WithErrorExtractor(dataExtractor{}))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "order.create")
		wlog.Error(ctx, errors.New("bad field"))
		end()
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON line: %v\noutput: %q", err, out)
	}
	errInfo := got["error"].(map[string]any)
	data, _ := errInfo["data"].(map[string]any)
	internal, _ := errInfo["internal"].(map[string]any)
	attrs, _ := errInfo["attrs"].(map[string]any)
	if data["field"] != "email" {
		t.Errorf("error.data = %v, want field=email", errInfo["data"])
	}
	if internal["row_id"] != "r-1" {
		t.Errorf("error.internal = %v, want row_id=r-1", errInfo["internal"])
	}
	if attrs["legacy"] != "kept" {
		t.Errorf("error.attrs = %v, want legacy=kept (Attrs must keep working)", errInfo["attrs"])
	}
}

// TestCore_ErrorData_ReturnsCurrent proves ErrorData returns the current error's Data,
// and nil when there is none.
func TestCore_ErrorData_ReturnsCurrent(t *testing.T) {
	log := wlog.New(wlog.WithErrorExtractor(dataExtractor{}))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "order.create")

	if got := wlog.ErrorData(ctx); got != nil {
		t.Errorf("ErrorData with no error = %v, want nil", got)
	}
	wlog.Error(ctx, errors.New("bad field"))
	if got := wlog.ErrorData(ctx); got["field"] != "email" {
		t.Errorf("ErrorData = %v, want field=email", got)
	}
	if got := wlog.ErrorData(context.Background()); got != nil {
		t.Errorf("ErrorData outside an event = %v, want nil", got)
	}
	captureStdout(t, end)
}

// TestCore_Errorf_RecordsAndReturns proves Errorf records the error it returns.
func TestCore_Errorf_RecordsAndReturns(t *testing.T) {
	ctx, finish := startEvent(t)

	err := wlog.Errorf(ctx, "bad %s %d", "field", 42)
	if err == nil || err.Error() != "bad field 42" {
		t.Fatalf("Errorf returned %v, want the built error", err)
	}
	errInfo, _ := finish()["error"].(map[string]any)
	if errInfo["message"] != "bad field 42" {
		t.Errorf("recorded error.message = %v, want bad field 42", errInfo["message"])
	}
}

// panickingExtractor panics on every error, which a logging call must survive.
type panickingExtractor struct{}

// Extract panics on purpose.
func (panickingExtractor) Extract(error) wlog.ErrorInfo { panic("extractor boom") }

// nilError is an error type whose value can be nil.
type nilError struct{ msg string }

// Error returns the message, and panics when the value is nil.
func (e *nilError) Error() string { return e.msg }

// TestCore_CORE7_ExtractorPanicIsolated proves that a panicking extractor never
// reaches the caller: the event carries the INTERNAL fallback, and OnError hears
// about the panic.
func TestCore_CORE7_ExtractorPanicIsolated(t *testing.T) {
	var reported []string
	var mu sync.Mutex

	log, rec := wlogtest.New(t,
		wlog.WithErrorExtractor(panickingExtractor{}),
		wlog.OnProblem(func(p wlog.Problem) {
			mu.Lock()
			defer mu.Unlock()
			reported = append(reported, p.Source+": "+p.Err.Error())
		}),
	)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "op")
	wlog.Error(ctx, errors.New("backend refused"))
	end()

	info, _ := rec.Last()["error"].(map[string]any)
	if info == nil {
		t.Fatalf("no error field on the event: %v", rec.Last())
	}
	if info["code"] != "INTERNAL" {
		t.Errorf("code = %v, want INTERNAL", info["code"])
	}
	if !strings.Contains(fmt.Sprint(info["message"]), "panicked") {
		t.Errorf("message = %v, want the panic text", info["message"])
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) == 0 {
		t.Error("OnError heard nothing about the panic")
	}
}

// TestCore_CORE7_TypedNilError proves that an error whose value is a nil pointer
// never reaches the extractor, so a nil dereference cannot panic the caller.
func TestCore_CORE7_TypedNilError(t *testing.T) {
	_, rec := wlogtest.New(t)
	log := wlog.New()

	var typedNil *nilError
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	wlog.Error(ctx, typedNil)
	end()

	if _, ok := rec.Last()["error"]; ok {
		t.Errorf("a typed nil error was recorded: %v", rec.Last()["error"])
	}
}

// TestCore_CORE8_OnErrorPanicIsolated proves that a panic inside OnError never
// reaches the caller of a logging call.
func TestCore_CORE8_OnErrorPanicIsolated(t *testing.T) {
	log := wlog.New(
		wlog.WithDrains(panickingDrain{}),
		wlog.OnProblem(func(wlog.Problem) { panic("onproblem boom") }),
	)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "user_id", "u1")
	end()
}

// panickingDrain panics on every event, which OnError has to survive.
type panickingDrain struct{}

// Send panics on purpose.
func (panickingDrain) Send(context.Context, map[string]any) { panic("drain boom") }

// stringCoder carries a stable code, the shape herr and similar libraries use.
type stringCoder struct{ code string }

// Error returns a message.
func (e stringCoder) Error() string { return "coded" }

// Code returns the stable code.
func (e stringCoder) Code() string { return e.code }

// anyCoder carries its code as any, which a catalog extractor may fill.
type anyCoder struct{ code any }

// Error returns a message.
func (e anyCoder) Error() string { return "coded-any" }

// Code returns the code.
func (e anyCoder) Code() any { return e.code }

// TestCore_CORE24_WrappedCatalogCode proves that the default extractor finds a
// Code method through wrapping, for both the string and the any form, and that it
// names the Go type of the error.
func TestCore_CORE24_WrappedCatalogCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "op")
	wlog.Error(ctx, fmt.Errorf("wrap: %w", stringCoder{code: "PAYMENT_DECLINED"}))
	end()

	info, _ := rec.Last()["error"].(map[string]any)
	if info["code"] != "PAYMENT_DECLINED" {
		t.Errorf("code = %v, want PAYMENT_DECLINED through the wrap", info["code"])
	}
	if info["type"] == nil || info["type"] == "" {
		t.Errorf("type = %v, want the Go type of the error", info["type"])
	}

	log2, rec2 := wlogtest.New(t)
	ctx2 := log2.WithContext(context.Background())
	ctx2, end2 := wlog.Start(ctx2, "op")
	wlog.Error(ctx2, anyCoder{code: 42})
	end2()

	info2, _ := rec2.Last()["error"].(map[string]any)
	if info2["code"] != "42" {
		t.Errorf("code = %v, want the 42 from Code() any", info2["code"])
	}
}

// TestCore_CORE24_JoinCauses proves that a joined error lists its causes.
func TestCore_CORE24_JoinCauses(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	joined := errors.Join(errors.New("first failed"), fmt.Errorf("second: %w", errors.New("inner")))
	ctx, end := wlog.Start(ctx, "op")
	wlog.Error(ctx, joined)
	end()

	info, _ := rec.Last()["error"].(map[string]any)
	causes, _ := info["causes"].([]any)
	if len(causes) == 0 {
		t.Fatalf("causes = %v, want the two causes", info["causes"])
	}
	if causes[0] != "first failed" {
		t.Errorf("causes[0] = %v, want first failed", causes[0])
	}
	joined2, err := json.Marshal(info["causes"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(joined2), "second") {
		t.Errorf("causes = %s, want the second cause too", joined2)
	}
}

// pkgErrorsLike carries a stack the way pkg/errors does, without the import.
type pkgErrorsLike struct{}

// Error returns a message.
func (pkgErrorsLike) Error() string { return "stacked" }

// StackTrace returns the program counters of the caller's frames, as pkg/errors
// captures them.
func (pkgErrorsLike) StackTrace() []uintptr {
	pc := make([]uintptr, 8)
	n := runtime.Callers(1, pc)
	return pc[:n]
}

// TestCore_CORE24_PkgErrorsStack proves that a stack which arrives as program
// counters is read through reflection, so core needs no import of the library.
func TestCore_CORE24_PkgErrorsStack(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "op")
	wlog.Error(ctx, fmt.Errorf("wrap: %w", pkgErrorsLike{}))
	end()

	info, _ := rec.Last()["error"].(map[string]any)
	if _, ok := info["stack"]; !ok {
		t.Errorf("stack missing, so the counters were not read: %v", info)
	}
}
