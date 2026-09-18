// This file tests debug mode and the runtime stats: every drop reason reports
// WLOG_EVENT_DROPPED with its reason, Stats counts what the Logger did, the debug
// handler serves those numbers as JSON, and a slow drain reports once.
package wlog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
)

// statsDrain is a drain that reports its own numbers, so Stats carries one entry for it.
type statsDrain struct{}

// Send counts nothing, because the numbers are the point of the test.
func (statsDrain) Send(context.Context, map[string]any) {}

// Stats reports a fixed queue shape.
func (statsDrain) Stats() wlog.DrainStats {
	return wlog.DrainStats{Queued: 2, Sent: 5, Dropped: 1, Retried: 3, LastError: "boom"}
}

// TestProblems_BET10_EveryDropReason proves that debug mode names the reason an event
// is missing, for every reason core knows.
func TestProblems_BET10_EveryDropReason(t *testing.T) {
	// everyDrop builds a logger with debug on, and the channel of its problems.
	everyDrop := func(t *testing.T, opts ...wlog.Option) (*wlog.Logger, chan wlog.Problem) {
		t.Helper()
		problems := make(chan wlog.Problem, 8)
		opts = append(opts,
			wlog.WithDebug(true),
			wlog.WithSilent(),
			wlog.OnProblem(func(p wlog.Problem) { problems <- p }),
		)
		return wlog.New(opts...), problems
	}
	// reasonOf closes the channel and returns the reason of the first drop report.
	reasonOf := func(t *testing.T, problems chan wlog.Problem) string {
		t.Helper()
		close(problems)
		for p := range problems {
			if p.Code == "WLOG_EVENT_DROPPED" {
				return p.Source
			}
		}
		return ""
	}

	t.Run("level", func(t *testing.T) {
		log, problems := everyDrop(t, wlog.WithLevel(wlog.LevelError))
		_, end := wlog.Start(log.WithContext(context.Background()), "op")
		end()
		if got := reasonOf(t, problems); got != "level" {
			t.Errorf("reason = %q, want level", got)
		}
	})

	t.Run("sampled", func(t *testing.T) {
		log, problems := everyDrop(t, wlog.WithHeadSampler(dropAll{}))
		_, end := wlog.Start(log.WithContext(context.Background()), "op")
		end()
		if got := reasonOf(t, problems); got != "sampled" {
			t.Errorf("reason = %q, want sampled", got)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		log, problems := everyDrop(t)
		log.SetEnabled(false)

		_, end := wlog.Start(log.WithContext(context.Background()), "op")
		end()
		if got := reasonOf(t, problems); got != "disabled" {
			t.Errorf("reason = %q, want disabled", got)
		}
	})

	t.Run("closed", func(t *testing.T) {
		log, problems := everyDrop(t)
		ctx := log.WithContext(context.Background())
		if err := log.Close(ctx); err != nil {
			t.Fatalf("Close: %v", err)
		}

		_, end := wlog.Start(ctx, "op")
		end()
		if got := reasonOf(t, problems); got != "closed" {
			t.Errorf("reason = %q, want closed", got)
		}
	})

	t.Run("too_large", func(t *testing.T) {
		log, problems := everyDrop(t)
		// A reserved group cannot be trimmed, so an event whose own group is over
		// the cap has to go.
		big := strings.Repeat("x", 64<<10)
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		wlog.SetGroup(ctx, "http", "a", big, "b", big, "c", big, "d", big, "e", big)
		end()
		if got := reasonOf(t, problems); got != "too_large" {
			t.Errorf("reason = %q, want too_large", got)
		}
	})
}

// TestStats_BET19_Counts proves that Stats counts the events a Logger emitted and the
// drops per reason, and carries one entry per drain that reports its own numbers.
func TestStats_BET19_Counts(t *testing.T) {
	log := wlog.New(wlog.WithSilent(), wlog.WithLevel(wlog.LevelWarn), wlog.WithDrains(statsDrain{}))
	ctx := log.WithContext(context.Background())

	kept, endKept := wlog.Start(ctx, "kept")
	wlog.SetLevel(kept, wlog.LevelWarn)
	endKept()

	_, endDropped := wlog.Start(ctx, "dropped")
	endDropped()

	stats := log.Stats()
	if stats.Emitted != 1 {
		t.Errorf("Emitted = %d, want 1", stats.Emitted)
	}
	if stats.Dropped["level"] != 1 {
		t.Errorf("Dropped[level] = %d, want 1: %v", stats.Dropped["level"], stats.Dropped)
	}
	if _, ok := stats.Dropped["sampled"]; ok {
		t.Errorf("Dropped carries a reason that never fired: %v", stats.Dropped)
	}
	if len(stats.Drains) != 1 {
		t.Fatalf("Drains = %v, want one entry", stats.Drains)
	}
	d := stats.Drains[0]
	if d.Name == "" || d.Queued != 2 || d.Sent != 5 || d.Dropped != 1 || d.Retried != 3 || d.LastError != "boom" {
		t.Errorf("drain stats = %+v, want the numbers the drain reported", d)
	}
}

// TestStats_DebugHandlerJSON proves that the debug handler serves Stats as one JSON
// object, so a debug route or a health check can read it.
func TestStats_DebugHandlerJSON(t *testing.T) {
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(statsDrain{}))
	_, end := wlog.Start(log.WithContext(context.Background()), "op")
	end()

	rec := httptest.NewRecorder()
	log.DebugHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/wlog", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Emitted int64             `json:"emitted"`
		Drains  []wlog.DrainStats `json:"drains"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("the body is not JSON: %v\nbody: %s", err, rec.Body)
	}
	if got.Emitted != 1 {
		t.Errorf("emitted = %d, want 1", got.Emitted)
	}
	if len(got.Drains) != 1 || got.Drains[0].Sent != 5 {
		t.Errorf("drains = %+v, want the drain's numbers", got.Drains)
	}
}

// TestProblems_DrainSlowOnce proves that a drain which blocks the emitting goroutine
// reports WLOG_DRAIN_SLOW once, however many events pay for it.
func TestProblems_DrainSlowOnce(t *testing.T) {
	problems := make(chan wlog.Problem, 8)
	slow := wlog.DrainFunc(func(context.Context, map[string]any) {
		time.Sleep(10 * time.Millisecond)
	})
	log := wlog.New(
		wlog.WithSilent(),
		wlog.WithDrains(slow),
		wlog.OnProblem(func(p wlog.Problem) { problems <- p }),
	)

	for i := 0; i < 3; i++ {
		_, end := wlog.Start(log.WithContext(context.Background()), "op")
		end()
	}

	close(problems)
	slowReports := 0
	for p := range problems {
		if p.Code == "WLOG_DRAIN_SLOW" {
			slowReports++
		}
	}
	if slowReports != 1 {
		t.Errorf("WLOG_DRAIN_SLOW reports = %d, want 1", slowReports)
	}
}
