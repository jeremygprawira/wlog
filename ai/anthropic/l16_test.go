package wloganthropic_test

import (
	"testing"

	"github.com/jeremygprawira/wlog"
	wloganthropic "github.com/jeremygprawira/wlog/ai/anthropic"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAnthropic_L16_NilStreamIsReported proves a nil stream is reported, not raised.
func TestAnthropic_L16_NilStreamIsReported(t *testing.T) {
	var got *wlog.Problem
	log, _ := wlogtest.New(t, wlog.OnProblem(func(p wlog.Problem) { got = &p }))
	prev := wlog.Default()
	wlog.SetDefault(log)
	t.Cleanup(func() { wlog.SetDefault(prev) })

	if wloganthropic.Observe(nil).Next() {
		t.Fatal("nil stream reported a chunk")
	}
	if got == nil {
		t.Fatal("the panic was not reported")
	}
}
