package wlog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// FuzzCore_SinksNeverLeak is gate G1 at the package level: a value stored under a
// denylisted key must never appear in either stdout sink's output — JSON or pretty —
// for any value the fuzzer produces. redact's own FuzzRedact_NeverLeaks already covers
// the redactor in isolation; this proves the full Start/Set/emit pipeline still
// applies it for both output formats.
func FuzzCore_SinksNeverLeak(f *testing.F) {
	f.Add("plain secret")
	f.Add("")
	f.Add("4111111111111111")
	f.Add("eyJhbGciOiJIUzI1NiIs.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U")

	jsonLog := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	prettyLog := wlog.New(wlog.WithFormat(wlog.FormatPretty))

	f.Fuzz(func(t *testing.T, value string) {
		// Below this length, the fuzzer routinely finds "leaks" that are really just
		// a digit or two coincidentally shared with the timestamp, duration, or
		// redact.fingerprint hash — noise, not a real property to assert on.
		if len(value) < 6 {
			return
		}
		for _, log := range []*wlog.Logger{jsonLog, prettyLog} {
			out := captureStdout(t, func() {
				ctx := log.WithContext(context.Background())
				ctx, end := wlog.Start(ctx, "op")
				wlog.Set(ctx, "password", value)
				end()
			})
			if strings.Contains(out, value) {
				t.Fatalf("secret leaked through a sink: value=%q output=%q", value, out)
			}
		}
	})
}
