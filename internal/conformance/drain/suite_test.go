// This file runs the drain conformance suite against every drain in the repository, so one
// v2 shape is a test result for each backend.
package drainconformance_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/axiom"
	"github.com/jeremygprawira/wlog/drain/betterstack"
	"github.com/jeremygprawira/wlog/drain/clickhouse"
	"github.com/jeremygprawira/wlog/drain/datadog"
	"github.com/jeremygprawira/wlog/drain/file"
	"github.com/jeremygprawira/wlog/drain/hyperdx"
	"github.com/jeremygprawira/wlog/drain/loki"
	"github.com/jeremygprawira/wlog/drain/memory"
	"github.com/jeremygprawira/wlog/drain/otlp"
	"github.com/jeremygprawira/wlog/drain/posthog"
	"github.com/jeremygprawira/wlog/drain/sentry"
	"github.com/jeremygprawira/wlog/drain/webhook"
	"github.com/jeremygprawira/wlog/internal/conformance"
	drainconformance "github.com/jeremygprawira/wlog/internal/conformance/drain"
	"github.com/jeremygprawira/wlog/internal/httpfake"
)

// TestConformance_PIPE25_EveryDrain runs the drain suite against every drain in the repo,
// so each backend proves that it reads the v2 event shape and leaks nothing.
func TestConformance_PIPE25_EveryDrain(t *testing.T) {
	drainconformance.Run(conformance.Tester{T: t}, everyDrain(t))
}

// everyDrain builds one case per drain.
func everyDrain(t *testing.T) []drainconformance.Case {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.ndjson")
	mem := memory.New(0)

	return []drainconformance.Case{
		{Name: "axiom", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return axiom.New(
				axiom.WithToken("tok"),
				axiom.WithDataset("logs"),
				axiom.WithURL(srv.URL),
			)
		}},
		{Name: "betterstack", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return betterstack.New(betterstack.WithSourceToken("tok"), betterstack.WithHost(srv.URL))
		}},
		{Name: "clickhouse", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return clickhouse.New(
				clickhouse.WithURL(srv.URL),
				clickhouse.WithBasicAuth("default", "pass"),
				clickhouse.WithDatabase("analytics"),
				clickhouse.WithTable("events"),
			)
		}},
		{Name: "datadog", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return datadog.New(datadog.WithURL(srv.URL), datadog.WithAPIKey("key"))
		}},
		{Name: "file", Build: func(*httpfake.Server) (wlog.Drain, error) {
			return file.New(file.WithPath(path))
		}, Local: func(*httpfake.Server) []byte {
			body, _ := os.ReadFile(path)
			return body
		}},
		{Name: "hyperdx", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return hyperdx.New(
				hyperdx.WithAPIKey("key"),
				hyperdx.WithEndpoint(srv.URL+"/v1/logs"),
				hyperdx.WithService("checkout"),
			)
		}},
		{Name: "loki", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return loki.New(loki.WithURL(srv.URL))
		}},
		{Name: "memory", Build: func(*httpfake.Server) (wlog.Drain, error) {
			return mem, nil
		}, Local: func(*httpfake.Server) []byte {
			body, _ := json.Marshal(mem.Snapshot())
			return body
		}, Check: func(t conformance.TB, name string, body []byte) {
			drainconformance.CheckV2Body(t, name, body)
			events := mem.Snapshot()
			if len(events) == 0 {
				t.Errorf("%s: no event was stored", name)
				return
			}
			if _, ok := events[0]["duration_ms"].(float64); !ok {
				t.Errorf("%s: duration_ms = %T, want float64", name, events[0]["duration_ms"])
			}
		}},
		{Name: "otlp", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return otlp.New(otlp.WithEndpoint(srv.URL))
		}},
		{Name: "posthog", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return posthog.New(posthog.WithAPIKey("key"), posthog.WithHost(srv.URL))
		}},
		{Name: "sentry", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return sentry.New(sentry.WithDSN(dsnFor(srv)))
		}},
		{Name: "webhook", Build: func(srv *httpfake.Server) (wlog.Drain, error) {
			return webhook.New(webhook.WithURL(srv.URL))
		}},
	}
}

// dsnFor builds a Sentry DSN that points at the fake server.
func dsnFor(srv *httpfake.Server) string {
	return "http://public-key@" + strings.TrimPrefix(srv.URL, "http://") + "/42"
}

// TestConformance_PIPE25_BrokenDrain proves that a drain which writes nothing, drops the
// v2 shape, or leaks a redacted value fails the suite with a readable report.
func TestConformance_PIPE25_BrokenDrain(t *testing.T) {
	capture := &conformance.Capture{}
	drainconformance.Run(capture, []drainconformance.Case{
		{Name: "no-output", Build: noopDrain, Local: func(*httpfake.Server) []byte { return nil }},
		{Name: "no-v2", Build: noopDrain, Local: func(*httpfake.Server) []byte {
			return []byte(`{"conformance_marker":"PIPE25-MARKER"}`)
		}},
		{Name: "leaks", Build: noopDrain, Local: func(*httpfake.Server) []byte {
			return []byte(`{"password":"PIPE25-s3cret"}`)
		}},
		{Name: "build-fails", Build: func(*httpfake.Server) (wlog.Drain, error) {
			return nil, errors.New("boom")
		}},
	})

	if len(capture.Failures) == 0 {
		t.Fatal("the broken drains passed the suite")
	}
	for _, name := range []string{"no-output", "no-v2", "leaks", "build-fails"} {
		if !capture.Reports(name) {
			t.Errorf("the broken drain did not fail %s:\n%s", name, strings.Join(capture.Failures, "\n"))
		}
	}
}

// noopDrain builds a drain that does nothing, so a local case can report its own bytes.
func noopDrain(*httpfake.Server) (wlog.Drain, error) {
	return wlog.DrainFunc(func(context.Context, map[string]any) {}), nil
}
