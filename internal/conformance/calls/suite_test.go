// This file runs the calls conformance suite against a reference adapter, and against a
// broken adapter that must fail it.
package callsconformance_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
)

// TestConformance_CallsFake proves that the reference adapter passes every calls scenario,
// and that an adapter which records no call fails them with a report.
func TestConformance_CallsFake(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, fake{})

	t.Run("BrokenFakeFails", func(t *testing.T) {
		broken := &conformance.Capture{}
		callsconformance.Run(broken, brokenFactory{})

		if len(broken.Failures) == 0 {
			t.Fatal("the broken adapter passed the suite")
		}
		for _, name := range []string{"Kinds", "FailedCall", "CapAndStats"} {
			if !broken.Reports(name) {
				t.Errorf("the broken adapter did not fail %s:\n%s", name, strings.Join(broken.Failures, "\n"))
			}
		}
	})
}

// fake starts one call, ends it, and returns the error of the call. It is the reference
// adapter for a client, a store, or a cache.
type fake struct{}

// Call opens one call, records the result, and returns the call error.
func (fake) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	_, end := wlog.StartCall(ctx, call)
	end(result)
	return result.Err
}

// brokenFactory records no call.
type brokenFactory struct{}

// Call records no call, and fails loudly.
func (brokenFactory) Call(context.Context, *wlog.Logger, wlog.Call, wlog.CallResult) error {
	return errors.New("broken adapter")
}
