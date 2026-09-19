# Spec: redact

> Module id `redact` · package `github.com/jeremygprawira/wlog/redact` · root module · depends on: nothing.
> Project-wide rules in [SPEC.md](SPEC.md) apply.
> v1.2 additions to this module: [SPEC-v1.2-additions.md](SPEC-v1.2-additions.md).

## Objective

Scrub sensitive data from a wide event before any sink sees it. The **denylist is a set the
user can add to and remove from**, with useful defaults. Replaces the boilerplate's
`internal/pkg/logger/masking.go` and fixes its problems:

| Boilerplate today | redact |
|---|---|
| Global `SensitiveFields` slice mutated by `AddSensitiveField` → data race under load | Immutable `*Redactor` value. Changes produce a new value, swapped atomically by core |
| Substring match: `auth` masks `author`, `cert` masks `concert` | Token match: `auth` masks `x-auth-token`, `authHeader`. Not `author` |
| Keys only. Secrets inside values (a JWT in a `note` field) leak | Keys **and** value patterns (JWT, card, email, phone, …) |
| Opt-in (`AddSafe`). Plain `Add` leaks | Runs on every event, always. No "unsafe" variant exists |

## Runtime policy (decided: hybrid C)

- A `*Redactor` never changes after `New`/`With` returns, so `Apply` needs no locks.
- Core stores the active redactor in an `atomic.Pointer[redact.Redactor]`:
  `wlog.WithRedactor(r)` at construction, `log.SetRedactor(next)` at runtime (for example, from remote config).
- An invalid config fails in `New`/`With` **before** it can be swapped in. The previous redactor stays active.
- Each emitted event records `redact.fingerprint` (short hash of the effective config), so you can
  audit which denylist was active for any log line. Disable with a core option.

## Behaviour

### Input contract

`Apply(event map[string]any)` receives a **private snapshot** owned by core. Core already
normalized it to a JSON tree (`map[string]any`, `[]any`, `string`, `float64`/`int64`, `bool`,
`nil`). It masks
**in place**. Structs are normalized by core via JSON tags first, so `json:"password"` is matched.
Key and path rules match **canonical** (default namespaced) key names. Core applies field
renaming presets *after* redaction, so denylist entries do not change with the output names.

### Stage order (per event)

1. **Transforms**: user funcs `func(event map[string]any)`, for example drop `http.request.body` for a regulated tenant.
2. **Key/path rules**: matched key → whole value (including nested maps/arrays) replaced with `"[REDACTED]"`.
3. **Built-in value patterns**: scan remaining strings. Partial masking (table below).
4. **Custom value patterns**: user regexes with string or func replacement.

A panic in a transform or replacement func is recovered. The affected value becomes
`"[REDACTED]"` (fail closed), and the event still emits.

### Key matching

- Case-insensitive. Keys are tokenized on `_ - . space` and camelCase boundaries:
  `accessToken` → `[access token]`, `X-Auth-Token` → `[x auth token]`, `HTTPAuthToken` → `[http auth token]`.
- An entry **without dot and without `*`** matches a token sequence that appears contiguously in
  the key's tokens, at any depth. `api_key` matches `stripe_api_key`, `apiKey`. `token` matches `refresh_token`.
- Entry **with `*`** → glob on the whole lowercased key within one segment: `*_pin`, `x-*-secret`.
- Entry **with dot** → path from event root, each segment matched as above: `http.request.headers.cookie`, `user.*`.
- Array elements inherit their parent path (`items.card_number` matches every element).

### Default key denylist

Ported from the boilerplate, deduplicated by tokenization (`apikey`/`api_key`/`api-key` → one entry):

```
password passwd pwd secret token auth authorization bearer api_key session cookie
credential private_key cert certificate credit_card card_number cvv cvc ssn
social_security aws_secret_access_key aws_access_key_id aws_session_token
connection_string db_password x_api_key pin otp
```

Changes vs boilerplate: removed `public_key` (not secret). Added `pin`, `otp`.
*Ask-first boundary: changing this list.*

### Built-in value patterns

| Name | Default | Example in | Out | Notes |
|---|---|---|---|---|
| `credit_card` | on | `4111111111111111` | `****1111` | 13–19 digits, spaces/dashes allowed, **Luhn-validated** |
| `email` | on | `alice@example.com` | `a***@***.com` | |
| `ipv4` | on | `192.168.1.100` | `***.***.***.100` | skips `127.0.0.1`, `0.0.0.0`. Core's own `http.client_ip` is exempt unless `MaskClientIP()` |
| `phone` | on | `+62 812-3456-7890`, `081234567890` | `+62 ****7890` | E.164 + Indonesian local `08…` |
| `jwt` | on | `eyJhbGciOi…` | `eyJ***.***` | three base64url segments |
| `bearer` | on | `Bearer sk_live_abc` | `Bearer ***` | |
| `iban` | on | `FR76 3000 6000 …189` | `FR76****189` | |
| `nik` | **off** | `3171234567890001` | `3171********0001` | Indonesian national ID. Opt-in via `EnablePatterns("nik")` (false-positive risk) |

### Public API

<!-- snippet:sketch -->
```go
type Redactor struct{ /* unexported, immutable after New */ }
type Option func(*config)

type Pattern struct {
    Name        string             // unique; used by RemovePatterns
    Regex       string             // compiled in New; invalid → error
    Replacement string             // used when Replace is nil; default "[REDACTED]"
    Replace     func(Match) string // optional; panic → "[REDACTED]"
}
type Match struct {
    Path   string   // "http.request.headers.authorization"
    Key    string   // "authorization"
    Value  string   // full matched text
    Groups []string // regex capture groups
}

func New(opts ...Option) (*Redactor, error)
func MustNew(opts ...Option) *Redactor
func Default() *Redactor                                     // defaults only; what core uses when no redactor is set
func Disabled() *Redactor                                    // explicit, greppable opt-out
func (r *Redactor) With(opts ...Option) (*Redactor, error)   // derive; r unchanged
func (r *Redactor) Apply(event map[string]any)
func (r *Redactor) Keys() []string                           // effective list, sorted
func (r *Redactor) Denies(key string) bool                   // key name alone: deny list + leaf globs
func (r *Redactor) Patterns() []string                       // effective pattern names, sorted
func (r *Redactor) Fingerprint() string                      // stable short hash of effective config

// Denylist: add / reduce
func AddKeys(keys ...string) Option
func RemoveKeys(keys ...string) Option       // from defaults or earlier AddKeys; unknown key → error
func ReplaceKeys(keys ...string) Option      // start from exactly this list (no defaults)
func AddPatterns(p ...Pattern) Option
func RemovePatterns(names ...string) Option  // built-ins by name too, e.g. "email"; unknown → error
func EnablePatterns(names ...string) Option  // opt-in built-ins, e.g. "nik"
func NoBuiltinPatterns() Option

// Behaviour
func Replacement(s string) Option            // default "[REDACTED]"
func Transform(fn func(map[string]any)) Option
func MaskClientIP() Option                   // also mask core's http.client_ip
func MaxDepth(n int) Option                  // default 16; deeper subtrees → "[REDACTED:DEPTH]"
func MaxStringScan(n int) Option             // default 64KB; longer strings: key rules only, value → "[REDACTED:TOO_LARGE]"
```

Usage:

<!-- snippet:sketch -->
```go
r := redact.MustNew(
    redact.AddKeys("nik", "*_pin", "http.request.headers.x-signature"),
    redact.RemoveKeys("session"),           // we log session ids on purpose
    redact.RemovePatterns("ipv4"),          // IPs needed for fraud analysis
    redact.EnablePatterns("nik"),
    redact.AddPatterns(redact.Pattern{Name: "midtrans_key", Regex: `SB-Mid-server-\w+`}),
)
log := wlog.New(wlog.WithRedactor(r))

// later, e.g. on a remote-config change:
next, err := r.With(redact.AddKeys("tenant_secret"))
if err == nil {
    log.SetRedactor(next)                   // atomic; in-flight events use whichever was active at emit
}
```

## Success Criteria

1. Every example in the key-matching section and pattern table is an executable test case.
2. `RemoveKeys("session")` → `session_id` emitted as-is. `AddKeys("nik")` → `customer.profile.nik` masked at depth 3.
3. `New`/`With` return an error (never panic) for: invalid regex, invalid glob, duplicate pattern name,
   removing an unknown key or pattern, or an unknown built-in name in `EnablePatterns`.
4. **G1:** `FuzzRedact_NeverLeaks`: for random nested events containing a denied key or a
   pattern-matching secret, the serialized output never contains the secret. 30s fuzz clean in CI.
5. **G2:** `Apply` from 64 goroutines on one `*Redactor` passes `-race`. `With` never mutates the parent.
   Concurrent `SetRedactor` + emit in core passes `-race` (tested in core, specified here).
6. A panicking `Replace`/`Transform` yields `"[REDACTED]"` for that value and does not propagate.
7. Defaults do **not** mask `author`, `concert`, `tokenizer_version`, `spin_count`. They **do** mask `authHeader`, `X-Auth-Token`, `user_password`, `login_pin`.
8. `Fingerprint()` is equal for equivalent configs regardless of option order, and differs after any add/remove.
9. Benchmark: 50-field, 3-level event with 10 string values ≤ 30µs/op and ≤ 10 allocs/op on M-series
   (fits inside the project's 55µs request budget. Tune after first measurement).
10. Zero imports outside the standard library. Passes on Go 1.23.

## Testing

- `redact/redact_test.go` (package `redact_test`): table tests for keys, paths, globs, patterns, options, errors.
- `redact/tokenize_test.go` (package `redact`): tokenizer edge cases (acronyms, digits, unicode).
- `redact/fuzz_test.go`: G1.
- `redact/bench_test.go`: criterion 9.
- `redact/example_test.go`: `ExampleNew`, `ExampleRedactor_With`, `ExamplePattern_replace`.

## Boundaries (module-specific)

- **Always:** compile everything in `New`/`With`. `Apply` does no regex compilation, no locking, no I/O.
- **Ask first:** changing defaults, mask formats or tokenization rules.
- **Never:** return the original value on any error path. Log from inside `redact`.

## Open Questions

None.
