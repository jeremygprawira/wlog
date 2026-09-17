# Spec: setup

> Module id `setup` · package `github.com/jeremygprawira/wlog/setup` · root module · phase 11 ·
> depends on: `core-default`, `core-problems`, `pipeline`, the phase 10 root drains. Later drains
> add their rows and factories in their own phase. Project-wide rules in
> [SPEC.md](SPEC.md) apply. Closes BET-16, PAR-1, PAR-19, and PAR-20.

## Objective

Let a deploy pick its backends and service identity through env vars, with no code change. A team
moving from evlog keeps its secret names. A missing credential turns one drain off and says so,
and it never crashes the app.

## Behavior

<!-- snippet:sketch -->
```go
type Factory struct {
	Name string                                     // the WLOG_DRAINS name, such as "kafka"
	New  func(env Env) (wlog.Drain, error)          // reads its own vars through env
	Vars []Var                                      // documented vars, for doctor and explain
}

type Var struct {
	Name     string   // WLOG_KAFKA_BROKERS
	Aliases  []string // names read when Name is unset, in order
	Required bool
	Secret   bool     // never printed by doctor
}

type Env interface{ Lookup(name string) (string, bool) }

func FromEnv(opts ...Option) wlog.Option
func With(f ...Factory) Option         // third-party drains, such as wlogkafkago.Factory()
func WithEnv(e Env) Option             // tests, default os.LookupEnv
func Resolve(opts ...Option) Report    // what FromEnv would do, for wlog doctor
```

- `FromEnv` reads `WLOG_OUTPUT`, then builds each drain named in `WLOG_DRAINS` (comma separated).
  Core `New` reads the logger vars and the service identity, so a Logger without setup still
  follows them. An unknown name reports `WLOG_INVALID_CONFIG`.
- A drain with a missing required var is skipped. `FromEnv` reports `WLOG_DRAIN_DISABLED` with
  the missing names, never their values.
- `WLOG_DRAINS` unset means no drains. Setup never guesses a drain from secrets alone.
- A code option wins over env, in any order of options.
- `With` takes explicit factories. There is no global registry, so no package state is added.
- `Resolve` returns every drain, every var with set or unset status, and every problem, for
  `wlog doctor` and `wlog explain env`.

### Service identity

| Field | Read in order |
|---|---|
| `service.name` | `WLOG_SERVICE`, `OTEL_SERVICE_NAME`, `SERVICE_NAME`, the main module path from `debug.ReadBuildInfo` |
| `service.version` | `WLOG_VERSION`, `APP_VERSION`, `SERVICE_VERSION`, `vcs.revision` (first 12 characters) from build info |
| `service.env` | `WLOG_ENV`, `APP_ENV`, `ENVIRONMENT`, `deployment.environment.name` from `OTEL_RESOURCE_ATTRIBUTES` |
| `service.instance` | `WLOG_INSTANCE`, `HOSTNAME`, `os.Hostname()` |

### Logger vars

| Var | Values | Default |
|---|---|---|
| `WLOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |
| `WLOG_FORMAT` | `auto`, `json`, `pretty` | `auto` |
| `WLOG_OUTPUT` | `default`, `flat`, `otel`, `ecs`, `gcp`, `datadog`, `emf` | `default` |
| `WLOG_DEBUG` | `1` turns debug problem reports on | off |
| `WLOG_DRAINS` | Drain names, comma separated | none |
| `GOOGLE_CLOUD_PROJECT` | The project id for the `gcp` preset trace field | none |

A bad value reports `WLOG_INVALID_CONFIG` and keeps the default.

### Built-in drain vars

| Drain | Required | Optional, with aliases after the first name |
|---|---|---|
| `axiom` | `AXIOM_TOKEN` or `AXIOM_API_KEY`, and `AXIOM_DATASET` | `AXIOM_URL` or `AXIOM_EDGE_URL`, `AXIOM_ORG_ID` |
| `loki` | `LOKI_URL` or `LOKI_ENDPOINT` | `LOKI_USERNAME` or `LOKI_USER`, `LOKI_PASSWORD` or `LOKI_API_KEY`, `LOKI_TENANT_ID` |
| `otlp` | `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`, `OTEL_EXPORTER_OTLP_ENDPOINT`, or `OTLP_ENDPOINT` | `OTEL_EXPORTER_OTLP_HEADERS` |
| `file` | none | `WLOG_FILE_PATH` (default `.wlog/logs/`), `WLOG_FILE_MAX_SIZE_MB`, `WLOG_FILE_MAX_AGE`, `WLOG_FILE_MAX_BACKUPS`, `WLOG_FILE_MAX_FILES` |
| `webhook` | `WLOG_WEBHOOK_URL` | `WLOG_WEBHOOK_SECRET` |
| `sentry` | `SENTRY_DSN` | `SENTRY_ALL_EVENTS` |
| `clickhouse` | `CLICKHOUSE_URL` or `CLICKHOUSE_ENDPOINT` | `CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD`, `CLICKHOUSE_DATABASE`, `CLICKHOUSE_TABLE` |
| `datadog` | `DD_API_KEY` or `DATADOG_API_KEY` | `DD_SITE` or `DATADOG_SITE`, `DD_SERVICE`, `DD_ENV` |
| `posthog` | `POSTHOG_API_KEY` | `POSTHOG_HOST`, `WLOG_POSTHOG_EVENT` |
| `betterstack` | `BETTERSTACK_SOURCE_TOKEN` or `BETTER_STACK_API_KEY` | `BETTERSTACK_INGESTING_HOST` or `BETTERSTACK_HOST` |
| `hyperdx` | `HYPERDX_API_KEY` | `HYPERDX_ENDPOINT` or `HYPERDX_OTLP_ENDPOINT`, `HYPERDX_SERVICE` |
| `honeycomb` (phase 14) | `HONEYCOMB_API_KEY` | `HONEYCOMB_DATASET`, `HONEYCOMB_API_URL` or `HONEYCOMB_API_ENDPOINT` |
| `elastic` (phase 14) | `ELASTICSEARCH_URL` or `OPENSEARCH_URL` | `ELASTICSEARCH_API_KEY`, `ELASTICSEARCH_USERNAME`, `ELASTICSEARCH_PASSWORD`, `ELASTICSEARCH_INDEX` |
| `splunk` (phase 14) | `SPLUNK_HEC_URL`, `SPLUNK_HEC_TOKEN` | `SPLUNK_INDEX`, `SPLUNK_SOURCETYPE` |
| `victorialogs` (phase 14) | `VICTORIALOGS_URL` | `VICTORIALOGS_STREAM_FIELDS`, `VICTORIALOGS_ACCOUNT_ID`, `VICTORIALOGS_PROJECT_ID` |
| `syslog` (phase 14) | `WLOG_SYSLOG_ADDR` | `WLOG_SYSLOG_NETWORK`, `WLOG_SYSLOG_APP_NAME`, `WLOG_SYSLOG_FACILITY`, `WLOG_SYSLOG_SD_ID` |
| `newrelic` (phase 14) | `NEW_RELIC_LICENSE_KEY` or `NEW_RELIC_API_KEY` | `NEW_RELIC_REGION` |
| `memory` | none | `WLOG_MEMORY_SIZE` |
| `cloudwatch` (phase 14) | `WLOG_CLOUDWATCH_GROUP`, through `cloudwatch.Factory(client)` | `WLOG_CLOUDWATCH_STREAM` |
| `kafka` (phase 13) | `WLOG_KAFKA_BROKERS`, `WLOG_KAFKA_TOPIC`, through `wlogkafkago.Factory()` | none. SASL and TLS need code |
| `nats` (phase 13) | `WLOG_NATS_URL`, `WLOG_NATS_SUBJECT`, through `wlognats.Factory()` | none. Credentials need code |

Each drain spec defines how its drain uses these vars. Each var name is final once v1.0.0 ships.

## Success criteria

1. With `WLOG_DRAINS=axiom,loki` and only Axiom vars set, the Logger has one Axiom drain, and one
   `WLOG_DRAIN_DISABLED` report names the missing `LOKI_URL`.
2. With `AXIOM_TOKEN` unset, `AXIOM_API_KEY` works. With both set, `AXIOM_TOKEN` wins.
3. With no service env vars, a test binary built from a module gets the module path as
   `service.name`.
4. `wlog.New(setup.FromEnv(), wlog.WithService("x", "1", "prod"))` gives `service.name` `x`.
5. `Resolve` output never holds the value of a var marked `Secret`.
6. `setup.With(fakeFactory)` builds a third-party drain from `WLOG_DRAINS=fake`.

## Testing

Table tests with `WithEnv` and a map-backed `Env`. No test reads the real process env.

## Boundaries

- **Always:** add a new drain's vars to the table above in the drain's own spec.
- **Ask first:** renaming a var or alias after v1.0.0.
- **Never:** print a secret value, or add a package-level registry.

## Open questions

None.
