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
