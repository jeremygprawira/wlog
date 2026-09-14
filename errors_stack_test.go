package wlog_test

import (
	"testing"

	"github.com/jeremygprawira/wlog"
)

// stackyError lets any caller (http-std's panic recovery, for one) attach a stack
// trace that the default extractor picks up without needing to know the concrete type.
type stackyError struct{ stack string }

func (e *stackyError) Error() string { return "boom" }
func (e *stackyError) Stack() string { return e.stack }

func TestCore_DefaultExtractor_PicksUpOptionalStack(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.Error(ctx, &stackyError{stack: "goroutine 1 [running]:\nmain.main()"})
	got := finish()

	errInfo := got["error"].(map[string]any)
	if errInfo["stack"] != "goroutine 1 [running]:\nmain.main()" {
		t.Errorf("error.stack = %v, want the Stack() value", errInfo["stack"])
	}
}

func TestCore_DefaultExtractor_NoStackMethod_LeavesStackEmpty(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.Error(ctx, errFmtOnly{})
	got := finish()

	errInfo := got["error"].(map[string]any)
	if _, ok := errInfo["stack"]; ok {
		t.Errorf("stack present for an error with no Stack() method: %v", errInfo["stack"])
	}
}

type errFmtOnly struct{}

func (errFmtOnly) Error() string { return "plain" }
