package wlog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
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

// secretBox carries a secret in a field that only the json tags name, so the copy
// path and the enricher path both have to walk a struct rather than a tree value.
type secretBox struct {
	Password string `json:"password"`
}

// FuzzCore_EventShapeNeverLeaks is gate G1 over the whole event shape: any key
// name, any nesting depth, any value type, a plain line, and a value an enricher
// adds. A secret that a caller or an enricher stores under a denylisted key must
// never appear in the recorder drain, in the JSON sink, or in the pretty sink.
//
// The target covers the paths that a single Set call does not: a struct behind a
// json tag, a nested map, an AppendLog line, and the enrich stage, where the
// canonical map changes after the event lock was released.
func FuzzCore_EventShapeNeverLeaks(f *testing.F) {
	f.Add("password", "hunter2-secret", uint8(0))
	f.Add("api_key", "sk-live-abcdef123456", uint8(2))
	f.Add("passphrase", "correct-horse-battery", uint8(1))
	f.Add("token", "eyJhbGciOiJIUzI1NiJ9.payload.sig", uint8(3))

	f.Fuzz(func(t *testing.T, key, value string, depth uint8) {
		// A short value arrives by chance inside a timestamp, inside a duration,
		// or inside a key name that the fuzzer built. The secret carries a marker
		// that no other text holds, so a hit is a real leak and never a
		// coincidence.
		secret := "SECRETMARKER-" + value
		if len(value) < 3 {
			return
		}

		// The secret sits under a denylisted key at every level, so the whole
		// value must vanish from every sink. The fuzzed key name rides along on a
		// field that carries no secret, which keeps the invariant true while the
		// shape of the event still varies.
		nested := any(secret)
		for i := uint8(0); i < depth%4; i++ {
			nested = map[string]any{"password": nested}
		}

		log, rec := wlogtest.New(t,
			wlog.WithEnrichers(wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
				event[key] = "shape"
				event["password"] = secretBox{Password: secret}
				event["enriched"] = map[string]any{"password": nested}
			})),
		)
		prettyLog := wlog.New(wlog.WithFormat(wlog.FormatPretty))

		stdout := ""
		for _, l := range []*wlog.Logger{log, prettyLog} {
			stdout += captureStdout(t, func() {
				ctx := l.WithContext(context.Background())
				ctx, end := wlog.Start(ctx, "op")
				wlog.Set(ctx, key, "shape")
				wlog.Set(ctx, "password", secret)
				wlog.Set(ctx, "nested_password", nested)
				wlog.Set(ctx, "box", secretBox{Password: secret})
				wlog.Append(ctx, "password", secret)
				wlog.Info(ctx, key, "password", secret)
				end()
			})
		}

		out, err := json.Marshal(rec.Events())
		if err != nil {
			t.Fatal(err)
		}
		for name, text := range map[string]string{"drain": string(out), "stdout": stdout} {
			if strings.Contains(text, secret) {
				t.Fatalf("secret leaked into %s: key=%q depth=%d value=%q output=%s", name, key, depth, secret, text)
			}
		}
	})
}
