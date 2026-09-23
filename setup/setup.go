// Package setup lets a deployment pick its drains and its service identity through
// environment variables, with no code change.
//
// FromEnv returns one wlog.Option: it builds every drain named in WLOG_DRAINS, applies
// WLOG_OUTPUT, and reports a drain it turns off. Resolve returns the same work as a
// report, so wlog doctor can print it. No package-level registry holds a drain, so With
// passes a third-party factory explicitly.
package setup

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/axiom"
	"github.com/jeremygprawira/wlog/drain/betterstack"
	"github.com/jeremygprawira/wlog/drain/clickhouse"
	"github.com/jeremygprawira/wlog/drain/datadog"
	"github.com/jeremygprawira/wlog/drain/file"
	"github.com/jeremygprawira/wlog/drain/honeycomb"
	"github.com/jeremygprawira/wlog/drain/hyperdx"
	"github.com/jeremygprawira/wlog/drain/loki"
	"github.com/jeremygprawira/wlog/drain/memory"
	"github.com/jeremygprawira/wlog/drain/newrelic"
	"github.com/jeremygprawira/wlog/drain/otlp"
	"github.com/jeremygprawira/wlog/drain/posthog"
	"github.com/jeremygprawira/wlog/drain/sentry"
	"github.com/jeremygprawira/wlog/drain/webhook"
	"github.com/jeremygprawira/wlog/preset"
)

// Factory builds one drain from the environment. Name is the WLOG_DRAINS name.
type Factory struct {
	Name string
	New  func(env Env) (wlog.Drain, error)
	Vars []Var
}

// Option configures one resolution.
type Option func(*config)

// config holds one resolution in progress.
type config struct {
	env     Env
	extra   []Factory
	factory []Factory
}

// WithEnv sets the environment reader. The default reads the process environment.
func WithEnv(e Env) Option { return func(c *config) { c.env = e } }

// With adds third-party factories. A name here wins over a built-in of the same name.
func With(factories ...Factory) Option {
	return func(c *config) { c.extra = append(c.extra, factories...) }
}

// Report is what FromEnv would do, for wlog doctor and wlog explain env. It never holds
// the value of a variable marked Secret.
type Report struct {
	Drains   []string      // the drain names that built
	Vars     []ResolvedVar // every variable, with set or unset status
	Problems []wlog.Problem
}

// ResolvedVar is one variable and whether the environment set it.
type ResolvedVar struct {
	Drain    string
	Name     string
	Set      bool
	Required bool
	Secret   bool
}

// resolution is the shared result of reading one environment.
type resolution struct {
	env      Env
	identity identity
	drains   []wlog.Drain
	names    []string
	vars     []ResolvedVar
	problems []wlog.Problem
	output   wlog.OutputPreset
}

// resolve reads the environment once, and returns what FromEnv and Resolve both need.
func resolve(opts ...Option) *resolution {
	c := &config{env: OSEnv{}}
	for _, opt := range opts {
		opt(c)
	}
	c.factory = append(c.extra, builtins()...)

	r := &resolution{env: c.env}
	r.identity = readIdentity(c.env)

	names := splitNames(firstOr(c.env, "", "WLOG_DRAINS"))
	if output, ok := first(c.env, "WLOG_OUTPUT"); ok {
		p, known := preset.ByName(output)
		if !known {
			r.problems = append(r.problems, problem("WLOG_INVALID_CONFIG", "WLOG_OUTPUT",
				fmt.Errorf("WLOG_OUTPUT: unknown preset %q, using default", output)))
		} else {
			r.output = p
		}
	}

	byName := map[string]Factory{}
	for _, f := range c.factory {
		byName[f.Name] = f
	}

	for _, name := range names {
		f, known := byName[name]
		if !known {
			r.problems = append(r.problems, problem("WLOG_INVALID_CONFIG", "WLOG_DRAINS",
				fmt.Errorf("WLOG_DRAINS: unknown drain %q", name)))
			continue
		}
		r.collectVars(f)
		if missing := missingVars(f, c.env); len(missing) > 0 {
			r.problems = append(r.problems, problem("WLOG_DRAIN_DISABLED", name,
				fmt.Errorf("the drain %s is off: %s is not set", name, strings.Join(missing, ", "))))
			continue
		}
		drain, err := f.New(c.env)
		if err != nil {
			r.problems = append(r.problems, problem("WLOG_DRAIN_DISABLED", name, err))
			continue
		}
		r.drains = append(r.drains, drain)
		r.names = append(r.names, name)
	}
	return r
}

// collectVars records every var of one selected factory, with its set status.
func (r *resolution) collectVars(f Factory) {
	for _, v := range f.Vars {
		_, set := v.lookup(r.env)
		r.vars = append(r.vars, ResolvedVar{
			Drain: f.Name, Name: v.Name, Set: set, Required: v.Required, Secret: v.Secret,
		})
	}
}

// FromEnv returns one Option that adds every drain named in WLOG_DRAINS, applies
// WLOG_OUTPUT and the service identity, and reports every drain it turned off. An
// explicit code option still wins, because it runs after this one when it comes later.
func FromEnv(opts ...Option) wlog.Option {
	r := resolve(opts...)
	return func(l *wlog.Logger) {
		wlog.WithDrains(r.drains...)(l)
		if r.output != nil {
			wlog.WithOutput(r.output)(l)
		}
		wlog.WithService(r.identity.name, r.identity.version, r.identity.env)(l)
		for _, p := range r.problems {
			l.Report(p)
		}
	}
}

// Resolve reads the environment and returns what FromEnv would do. The report holds no
// secret value.
func Resolve(opts ...Option) Report {
	r := resolve(opts...)
	return Report{Drains: r.names, Vars: r.vars, Problems: r.problems}
}

// problem builds one report with a stable code. The catalog fills Why, Fix, and Link.
func problem(code, source string, err error) wlog.Problem {
	return wlog.Problem{Code: code, Source: source, Message: err.Error(), Err: err}
}

// splitNames splits a comma-separated list and trims each name.
func splitNames(value string) []string {
	var out []string
	for _, name := range strings.Split(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// missingVars returns the names of every required variable that holds no value.
func missingVars(f Factory, env Env) []string {
	var out []string
	for _, v := range f.Vars {
		if v.Required {
			out = append(out, v.missing(env)...)
		}
	}
	return out
}

// intOr reads an integer, and returns the fallback when it is unset or bad.
func intOr(env Env, fallback int, names ...string) int {
	value, ok := first(env, names...)
	if !ok {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return number
}

// durationOr reads a Go duration, and returns the fallback when it is unset or bad.
func durationOr(env Env, fallback time.Duration, names ...string) time.Duration {
	value, ok := first(env, names...)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}

// Builtins returns the factory of every drain that ships in this repository, so a tool
// can list every variable before a deploy picks a drain.
func Builtins() []Factory { return builtins() }

// builtins returns the factories of every drain that ships in this repository. A later
// phase adds its own drain here, or a caller passes it with With.
func builtins() []Factory {
	return []Factory{
		{
			Name: "axiom",
			Vars: []Var{
				required("AXIOM_TOKEN", "AXIOM_API_KEY"),
				required("AXIOM_DATASET"),
				optional("AXIOM_URL", "AXIOM_EDGE_URL"),
			},
			New: func(env Env) (wlog.Drain, error) {
				token, _ := first(env, "AXIOM_TOKEN", "AXIOM_API_KEY")
				dataset, _ := first(env, "AXIOM_DATASET")
				opts := []axiom.Option{axiom.WithToken(token), axiom.WithDataset(dataset)}
				if url, ok := first(env, "AXIOM_URL", "AXIOM_EDGE_URL"); ok {
					opts = append(opts, axiom.WithURL(url))
				}
				return axiom.New(opts...)
			},
		},
		{
			Name: "betterstack",
			Vars: []Var{
				required("BETTERSTACK_SOURCE_TOKEN", "BETTER_STACK_API_KEY"),
				optional("BETTERSTACK_INGESTING_HOST", "BETTERSTACK_HOST"),
			},
			New: func(env Env) (wlog.Drain, error) {
				token, _ := first(env, "BETTERSTACK_SOURCE_TOKEN", "BETTER_STACK_API_KEY")
				opts := []betterstack.Option{betterstack.WithSourceToken(token)}
				if host, ok := first(env, "BETTERSTACK_INGESTING_HOST", "BETTERSTACK_HOST"); ok {
					opts = append(opts, betterstack.WithHost(host))
				}
				return betterstack.New(opts...)
			},
		},
		{
			Name: "clickhouse",
			Vars: []Var{
				required("CLICKHOUSE_URL", "CLICKHOUSE_ENDPOINT"),
				optional("CLICKHOUSE_USER"),
				{Name: "CLICKHOUSE_PASSWORD", Secret: true},
				optional("CLICKHOUSE_DATABASE"),
				optional("CLICKHOUSE_TABLE"),
			},
			New: func(env Env) (wlog.Drain, error) {
				url, _ := first(env, "CLICKHOUSE_URL", "CLICKHOUSE_ENDPOINT")
				user, _ := first(env, "CLICKHOUSE_USER")
				password, _ := first(env, "CLICKHOUSE_PASSWORD")
				opts := []clickhouse.Option{clickhouse.WithURL(url)}
				if user != "" || password != "" {
					opts = append(opts, clickhouse.WithBasicAuth(user, password))
				}
				if database, ok := first(env, "CLICKHOUSE_DATABASE"); ok {
					opts = append(opts, clickhouse.WithDatabase(database))
				}
				if table, ok := first(env, "CLICKHOUSE_TABLE"); ok {
					opts = append(opts, clickhouse.WithTable(table))
				}
				return clickhouse.New(opts...)
			},
		},
		{
			Name: "datadog",
			Vars: []Var{
				required("DD_API_KEY", "DATADOG_API_KEY"),
				optional("DD_SITE", "DATADOG_SITE"),
			},
			New: func(env Env) (wlog.Drain, error) {
				key, _ := first(env, "DD_API_KEY", "DATADOG_API_KEY")
				opts := []datadog.Option{datadog.WithAPIKey(key)}
				if site, ok := first(env, "DD_SITE", "DATADOG_SITE"); ok {
					opts = append(opts, datadog.WithSite(site))
				}
				return datadog.New(opts...)
			},
		},
		{
			Name: "file",
			Vars: []Var{
				optional("WLOG_FILE_PATH"),
				optional("WLOG_FILE_MAX_SIZE_MB"),
				optional("WLOG_FILE_MAX_AGE"),
				optional("WLOG_FILE_MAX_BACKUPS"),
				optional("WLOG_FILE_MAX_FILES"),
			},
			New: func(env Env) (wlog.Drain, error) {
				var opts []file.Option
				if path, ok := first(env, "WLOG_FILE_PATH"); ok {
					opts = append(opts, file.WithPath(path))
				}
				if size := intOr(env, 0, "WLOG_FILE_MAX_SIZE_MB"); size > 0 {
					opts = append(opts, file.WithMaxSize(int64(size)<<20))
				}
				if age := durationOr(env, 0, "WLOG_FILE_MAX_AGE"); age > 0 {
					opts = append(opts, file.WithMaxAge(age))
				}
				if backups := intOr(env, 0, "WLOG_FILE_MAX_BACKUPS"); backups > 0 {
					opts = append(opts, file.WithMaxBackups(backups))
				}
				if files := intOr(env, 0, "WLOG_FILE_MAX_FILES"); files > 0 {
					opts = append(opts, file.WithMaxFiles(files))
				}
				return file.New(opts...)
			},
		},
		{
			Name: "hyperdx",
			Vars: []Var{
				required("HYPERDX_API_KEY"),
				optional("HYPERDX_ENDPOINT", "HYPERDX_OTLP_ENDPOINT"),
				optional("HYPERDX_SERVICE"),
			},
			New: func(env Env) (wlog.Drain, error) {
				key, _ := first(env, "HYPERDX_API_KEY")
				opts := []hyperdx.Option{hyperdx.WithAPIKey(key)}
				if endpoint, ok := first(env, "HYPERDX_ENDPOINT", "HYPERDX_OTLP_ENDPOINT"); ok {
					opts = append(opts, hyperdx.WithEndpoint(endpoint))
				}
				if service, ok := first(env, "HYPERDX_SERVICE"); ok {
					opts = append(opts, hyperdx.WithService(service))
				}
				return hyperdx.New(opts...)
			},
		},
		{
			Name: "loki",
			Vars: []Var{
				required("LOKI_URL", "LOKI_ENDPOINT"),
				optional("LOKI_USERNAME", "LOKI_USER"),
				{Name: "LOKI_PASSWORD", Aliases: []string{"LOKI_API_KEY"}, Secret: true},
				optional("LOKI_TENANT_ID"),
			},
			New: func(env Env) (wlog.Drain, error) {
				url, _ := first(env, "LOKI_URL", "LOKI_ENDPOINT")
				user, _ := first(env, "LOKI_USERNAME", "LOKI_USER")
				password, _ := first(env, "LOKI_PASSWORD", "LOKI_API_KEY")
				opts := []loki.Option{loki.WithURL(url)}
				if user != "" || password != "" {
					opts = append(opts, loki.WithBasicAuth(user, password))
				}
				if tenant, ok := first(env, "LOKI_TENANT_ID"); ok {
					opts = append(opts, loki.WithTenantID(tenant))
				}
				return loki.New(opts...)
			},
		},
		{
			Name: "memory",
			Vars: []Var{optional("WLOG_MEMORY_SIZE")},
			New: func(env Env) (wlog.Drain, error) {
				return memory.New(intOr(env, 0, "WLOG_MEMORY_SIZE")), nil
			},
		},
		{
			Name: "otlp",
			Vars: []Var{
				required("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTLP_ENDPOINT"),
				optional("OTEL_EXPORTER_OTLP_HEADERS"),
			},
			New: func(env Env) (wlog.Drain, error) {
				endpoint, _ := first(env, "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTLP_ENDPOINT")
				opts := []otlp.Option{otlp.WithEndpoint(endpoint)}
				if headers, ok := first(env, "OTEL_EXPORTER_OTLP_HEADERS"); ok {
					opts = append(opts, otlp.WithHeaders(pairs(headers)))
				}
				return otlp.New(opts...)
			},
		},
		{
			Name: "posthog",
			Vars: []Var{
				required("POSTHOG_API_KEY"),
				optional("POSTHOG_HOST"),
				optional("WLOG_POSTHOG_EVENT"),
			},
			New: func(env Env) (wlog.Drain, error) {
				key, _ := first(env, "POSTHOG_API_KEY")
				opts := []posthog.Option{posthog.WithAPIKey(key)}
				if host, ok := first(env, "POSTHOG_HOST"); ok {
					opts = append(opts, posthog.WithHost(host))
				}
				if event, ok := first(env, "WLOG_POSTHOG_EVENT"); ok {
					opts = append(opts, posthog.WithEvent(event))
				}
				return posthog.New(opts...)
			},
		},
		{
			Name: "sentry",
			Vars: []Var{
				required("SENTRY_DSN"),
				optional("SENTRY_ALL_EVENTS"),
			},
			New: func(env Env) (wlog.Drain, error) {
				dsn, _ := first(env, "SENTRY_DSN")
				opts := []sentry.Option{sentry.WithDSN(dsn)}
				if value, ok := first(env, "SENTRY_ALL_EVENTS"); ok {
					opts = append(opts, sentry.WithAllEvents(value == "1" || strings.EqualFold(value, "true")))
				}
				return sentry.New(opts...)
			},
		},
		{
			Name: "webhook",
			Vars: []Var{
				required("WLOG_WEBHOOK_URL"),
				{Name: "WLOG_WEBHOOK_SECRET", Secret: true},
			},
			New: func(env Env) (wlog.Drain, error) {
				url, _ := first(env, "WLOG_WEBHOOK_URL")
				opts := []webhook.Option{webhook.WithURL(url)}
				if secret, ok := first(env, "WLOG_WEBHOOK_SECRET"); ok {
					opts = append(opts, webhook.WithSecret(secret))
				}
				return webhook.New(opts...)
			},
		},
		{
			Name: "honeycomb",
			Vars: []Var{
				required("HONEYCOMB_API_KEY"),
				optional("HONEYCOMB_DATASET"),
				optional("HONEYCOMB_API_URL", "HONEYCOMB_API_ENDPOINT"),
			},
			New: func(env Env) (wlog.Drain, error) {
				key, _ := first(env, "HONEYCOMB_API_KEY")
				opts := []honeycomb.Option{honeycomb.WithAPIKey(key)}
				if dataset, ok := first(env, "HONEYCOMB_DATASET"); ok {
					opts = append(opts, honeycomb.WithDataset(dataset))
				}
				if apiURL, ok := first(env, "HONEYCOMB_API_URL", "HONEYCOMB_API_ENDPOINT"); ok {
					opts = append(opts, honeycomb.WithAPIURL(apiURL))
				}
				return honeycomb.New(opts...)
			},
		},
		{
			Name: "newrelic",
			Vars: []Var{
				required("NEW_RELIC_LICENSE_KEY", "NEW_RELIC_API_KEY"),
				optional("NEW_RELIC_REGION"),
			},
			New: func(env Env) (wlog.Drain, error) {
				key, _ := first(env, "NEW_RELIC_LICENSE_KEY", "NEW_RELIC_API_KEY")
				opts := []newrelic.Option{newrelic.WithLicenseKey(key)}
				if region, ok := first(env, "NEW_RELIC_REGION"); ok {
					opts = append(opts, newrelic.WithRegion(region))
				}
				return newrelic.New(opts...)
			},
		},
	}
}
