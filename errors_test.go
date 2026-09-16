package wlog_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jeremygprawira/wlog"
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
