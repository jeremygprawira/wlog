package pipeline_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
)

func TestPipeline_Overflow_DropsOldest(t *testing.T) {
	// hangCh is closed explicitly, before Close is ever called: Close waits for the
	// worker to finish its current flush, so calling it while the worker is stuck in
	// SendBatch (as it is throughout this test) would deadlock the test itself.
	hangCh := make(chan struct{})
	sender := &fakeSender{hang: true, hangCh: hangCh}

	drop := &dropCapture{}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(1), pipeline.BatchInterval(time.Hour),
		pipeline.MaxBuffer(3),
		pipeline.OnDropped(drop.record),
	)

	// The first Send triggers a flush that hangs forever in sender.SendBatch,
	// holding one "in flight" batch slot. The next MaxBuffer(3) sends then fill
	// the buffer; anything beyond that must drop the oldest still-queued event.
	drain.Send(context.Background(), mkEvent(0))
	time.Sleep(20 * time.Millisecond) // let the hang start

	for i := 1; i <= 5; i++ {
		drain.Send(context.Background(), mkEvent(i))
	}

	waitFor(t, time.Second, func() bool { return drop.count() > 0 })
	events, _ := drop.first()
	if events[0]["i"] != 1 {
		t.Errorf("first dropped event = %v, want the oldest queued one (i=1)", events[0])
	}
	close(hangCh)
}

func TestPipeline_Overflow_NeverBlocksSend(t *testing.T) {
	hangCh := make(chan struct{})
	sender := &fakeSender{hang: true, hangCh: hangCh}

	drain := pipeline.Wrap(sender, pipeline.BatchSize(1), pipeline.MaxBuffer(2))

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			drain.Send(context.Background(), mkEvent(i))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Send blocked with a hanging Sender and a full buffer")
	}
	close(hangCh)
}

func TestPipeline_FanOut_DeliversToAll(t *testing.T) {
	var mu sync.Mutex
	var gotA, gotB bool
	a := wlog.DrainFunc(func(_ context.Context, e map[string]any) {
		mu.Lock()
		gotA = true
		mu.Unlock()
	})
	b := wlog.DrainFunc(func(_ context.Context, e map[string]any) {
		mu.Lock()
		gotB = true
		mu.Unlock()
	})

	fanned := pipeline.FanOut(a, b)
	fanned.Send(context.Background(), mkEvent(1))

	// FanOut.Send dispatches to each drain in its own goroutine and returns
	// immediately (gate G3), so delivery must be awaited, not checked synchronously
	// right after Send returns.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotA && gotB
	})
}

func TestPipeline_FanOut_OneHangingDrainDoesNotBlockOthers(t *testing.T) {
	hang := wlog.DrainFunc(func(context.Context, map[string]any) {
		select {} // hangs forever
	})
	fast := make(chan struct{}, 1)
	fastDrain := wlog.DrainFunc(func(context.Context, map[string]any) {
		fast <- struct{}{}
	})

	fanned := pipeline.FanOut(hang, fastDrain)
	go fanned.Send(context.Background(), mkEvent(1))

	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("a hanging drain blocked delivery to a fast one")
	}
}
