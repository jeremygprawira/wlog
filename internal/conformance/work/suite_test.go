// This file runs the work conformance suite against a reference adapter, and against a
// broken adapter that must fail it.
package workconformance_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/work"
)

// TestConformance_WorkFakes proves that the reference adapter passes every work scenario,
// and that an adapter which never starts a unit of work fails them with a report.
func TestConformance_WorkFakes(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, fake{})

	t.Run("BrokenFakeFails", func(t *testing.T) {
		broken := &conformance.Capture{}
		workconformance.Run(broken, brokenFactory{})

		if len(broken.Failures) == 0 {
			t.Fatal("the broken adapter passed the suite")
		}
		for _, name := range []string{"Kinds", "HandlerError", "StarterAndFinisher"} {
			if !broken.Reports(name) {
				t.Errorf("the broken adapter did not fail %s:\n%s", name, strings.Join(broken.Failures, "\n"))
			}
		}
	})
}

// TestConformance_DeclaringFactory proves that a factory which declares one kind runs the
// scenarios of that kind only, with the declared system and delivery count.
func TestConformance_DeclaringFactory(t *testing.T) {
	capture := &conformance.Capture{}
	workconformance.Run(capture, declaringFactory{})
	if len(capture.Failures) > 0 {
		t.Errorf("the declaring adapter failed the suite:\n%s", strings.Join(capture.Failures, "\n"))
	}
}

// declaringFactory produces the message kind only, and it declares that, so it covers the
// declaration paths of the suite.
type declaringFactory struct{}

// Declare names one kind and a system that is not the kafka default.
func (declaringFactory) Declare() workconformance.Declaration {
	return workconformance.Declaration{
		Kinds: []work.Kind{work.KindMessage}, System: "aws_sqs", DeliveryCount: true,
	}
}

// Process runs one unit of work through the work package, like the reference adapter.
func (declaringFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return work.Run(context.Background(), log, unit, handler, work.RecoverPanics())
}

// fake runs one unit of work through the work package, which is the reference adapter.
type fake struct{}

// Process opens one unit, runs the handler, and returns what the handler returned. It
// recovers a panic, so the suite continues after the panic scenario.
func (fake) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return work.Run(context.Background(), log, unit, handler, work.RecoverPanics())
}

// brokenFactory never starts a unit of work, so no scenario sees an event.
type brokenFactory struct{}

// Process returns a fixed error and records nothing.
func (brokenFactory) Process(*wlog.Logger, work.Unit, func(context.Context) error) error {
	return errors.New("broken adapter")
}
