// This file runs the work conformance suite against the activity path, and checks the
// fields, the asynchronous result, and the panic rule of the activity interceptor.
package wlogtemporal

import (
	"context"
	"fmt"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"

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

// workFactory drives the real activity interceptor with an activity info, so a change that
// breaks the adapter fails the suite.
type workFactory struct{}

// Declare names the one kind an activity attempt produces. The activity info carries the
// attempt.
func (workFactory) Declare() workconformance.Declaration {
	return workconformance.Declaration{Kinds: []work.Kind{work.KindJob}, Attempt: true}
}

// Process runs one unit of work through the interceptor. The suite expects no panic from
// Process, so the panic of the handler, which the interceptor raises again, comes back as an
// error.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	name, _ := unit.Fields["name"].(string)
	attempt, _ := unit.Fields["attempt"].(int)
	info := activity.Info{
		ActivityType:  activity.Type{Name: name},
		Attempt:       int32(attempt),
		ScheduledTime: unit.StartedAt,
	}
	a := &activityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{
			Next: &suiteNext{fn: handler},
		},
		log: log,
	}
	_, err = a.execute(context.Background(), info, nil)
	return err
}

// suiteNext runs the handler of the suite as the next activity interceptor.
type suiteNext struct {
	interceptor.ActivityInboundInterceptorBase
	fn func(context.Context) error
}

// ExecuteActivity runs the handler of the suite.
func (n *suiteNext) ExecuteActivity(context.Context, *interceptor.ExecuteActivityInput) (any, error) {
	return nil, n.fn(context.Background())
}
