// Package drainconformance is the conformance suite for every backend drain. It sends one
// v2-shaped event through a drain, and checks that the drain delivered the event in full
// and never leaked a redacted value.
//
// The core part runs for every drain: a v2 event reaches the wire, and a secret under a
// denied key never does. The wire part runs for every drain that writes bytes.
package drainconformance

import (
	"bytes"
	"context"
	"errors"
	"regexp"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/internal/httpfake"
)

// marker is the value the suite puts in a user key, so it can see that the event arrived.
const marker = "PIPE25-MARKER"

// Redacted is the value the suite hides under a denied key. It must never reach the wire.
const Redacted = "PIPE25-s3cret"

// uuidV7 matches the event id that core emits, so the suite can prove that a drain kept it.
var uuidV7 = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

// errPIPE25 is the error the suite records, so a drain that forwards error events only
// still sends the event.
var errPIPE25 = errors.New("PIPE25 failure")

// Case is one drain under test.
type Case struct {
	Name string
	// Build returns the drain pointed at srv, or at a local sink.
	Build func(srv *httpfake.Server) (wlog.Drain, error)
	// Local returns the bytes a local drain wrote. Nil for a drain that posts a request.
	Local func(srv *httpfake.Server) []byte
	// Check proves that one body reads the v2 shape. Nil uses CheckV2Body.
	Check func(t conformance.TB, name string, body []byte)
}

// Run runs every case.
func Run(t conformance.TB, cases []Case) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t conformance.TB) { runCase(t, c) })
	}
}

// runCase sends one v2 event through one drain and checks what it wrote.
func runCase(t conformance.TB, c Case) {
	t.Helper()
	srv := httpfake.New()
	defer srv.Close()

	drain, err := c.Build(srv)
	if err != nil {
		t.Errorf("%s: build the drain: %v", c.Name, err)
		return
	}
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(drain))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "PIPE25")
	wlog.Set(ctx, "conformance_marker", marker)
	wlog.Set(ctx, "password", Redacted)
	// The event is an error, because a drain such as Sentry forwards an error event only.
	wlog.Error(ctx, errPIPE25)
	end()
	if err := log.Flush(ctx); err != nil {
		t.Errorf("%s: Flush: %v", c.Name, err)
	}
	if err := log.Close(ctx); err != nil {
		t.Errorf("%s: Close: %v", c.Name, err)
	}

	body := bodyOf(c, srv)
	if len(body) == 0 {
		t.Errorf("%s: the drain wrote nothing", c.Name)
		return
	}
	if !bytes.Contains(body, []byte(marker)) {
		t.Errorf("%s: the event did not reach the wire", c.Name)
	}
	if bytes.Contains(body, []byte(Redacted)) {
		t.Errorf("%s: the redacted value reached the wire", c.Name)
	}
	check := c.Check
	if check == nil {
		check = CheckV2Body
	}
	check(t, c.Name, body)
}

// CheckV2Body proves that one body reads the v2 shape: an event id, a float duration, and
// a nested wlog object.
func CheckV2Body(t conformance.TB, name string, body []byte) {
	if !uuidV7.Match(body) {
		t.Errorf("%s: the body carries no event_id, so the drain lost the v2 shape", name)
	}
	for _, key := range []string{"duration_ms", "wlog"} {
		if !bytes.Contains(body, []byte(key)) {
			t.Errorf("%s: the body carries no %s, so the drain lost the v2 shape", name, key)
		}
	}
}

// bodyOf returns every byte the drain wrote, from the fake server or from a local drain.
func bodyOf(c Case, srv *httpfake.Server) []byte {
	if c.Local != nil {
		return c.Local(srv)
	}
	var out []byte
	for _, req := range srv.Requests() {
		out = append(out, req.Body...)
	}
	return out
}
