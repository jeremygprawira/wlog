// This file runs the work conformance suite against the activity path, and checks the
// fields, the asynchronous result, and the panic rule of the activity interceptor.
package wlogtemporal

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/work"
)

// TestTemporal_C1_WorkConformance proves that the activity path passes every scenario of the
// work suite.
func TestTemporal_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path the activity interceptor uses. The suite
// supplies the unit, because one activity carries no rpc, message, command, or function
// field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}
