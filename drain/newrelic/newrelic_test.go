package newrelic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/newrelic"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// fakeNewRelic records every request body.
type fakeNewRelic struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

// newFake starts a fake New Relic that answers 202.
func newFake(t *testing.T) *fakeNewRelic {
	t.Helper()
	f := &fakeNewRelic{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(f.Close)
	return f
}

// last returns the most recent request body.
func (f *fakeNewRelic) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return ""
	}
	return f.bodies[len(f.bodies)-1]
}

// requestEvent returns the one event the golden body holds.
func requestEvent() map[string]any {
	return map[string]any{
		"timestamp": "2026-09-22T10:00:00Z",
		"level":     "info",
		"summary":   "GET /orders",
		"operation": "GET /orders",
		"kind":      "request",
		"outcome":   "success",
		"service":   map[string]any{"name": "checkout"},
		"trace":     map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736", "span_id": "00f067aa0ba902b7"},
		"http":      map[string]any{"method": "GET", "route": "/orders", "status": int64(200)},
	}
}

// TestNewRelic_GoldenBody proves the request body matches the golden written from the New
// Relic documents.
func TestNewRelic_GoldenBody(t *testing.T) {
	srv := newFake(t)
	sender, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	golden, err := os.ReadFile("testdata/request.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if body := srv.last(); body != strings.TrimRight(string(golden), "\n") {
		t.Errorf("request body =\n%s\nwant\n%s", body, golden)
	}
}

// TestNewRelic_ReservedUserKeys proves a reserved user key moves under wlog.fields.
func TestNewRelic_ReservedUserKeys(t *testing.T) {
	srv := newFake(t)
	sender, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	event := requestEvent()
	event["accountId"] = "42"
	event["entity"] = map[string]any{"guid": "abc"}
	if err := sender.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	attributes := attributesOf(t, srv.last())
	if _, ok := attributes["accountId"]; ok {
		t.Error("accountId was not moved")
	}
	if attributes["wlog.fields.accountId"] != "42" {
		t.Errorf("wlog.fields.accountId = %v, want 42", attributes["wlog.fields.accountId"])
	}
	if attributes["wlog.fields.entity.guid"] != "abc" {
		t.Errorf("wlog.fields.entity.guid = %v, want abc", attributes["wlog.fields.entity.guid"])
	}
}

// TestNewRelic_AttributeCap proves a log keeps at most 255 attributes and reports the rest.
func TestNewRelic_AttributeCap(t *testing.T) {
	srv := newFake(t)
	sender, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	var problems []wlog.Problem
	log, _ := wlogtest.New(t, wlog.OnProblem(func(p wlog.Problem) { problems = append(problems, p) }))
	if err := sender.Setup(log); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	event := map[string]any{"summary": "big"}
	for i := 0; i < 300; i++ {
		event["key_"+string(rune('a'+i%26))+string(rune('a'+i/26))] = i
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := len(attributesOf(t, srv.last())); got != 255 {
		t.Errorf("attributes = %d, want 255", got)
	}
	caps := 0
	for _, p := range problems {
		if p.Code == "WLOG_CAP_REACHED" {
			caps++
		}
	}
	if caps != 1 {
		t.Errorf("WLOG_CAP_REACHED reported %d times, want 1", caps)
	}
}

// attributesOf reads the attributes of the one log in a request body.
func attributesOf(t *testing.T, body string) map[string]any {
	t.Helper()
	var envelopes []struct {
		Logs []struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"logs"`
	}
	if err := json.Unmarshal([]byte(body), &envelopes); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(envelopes) != 1 || len(envelopes[0].Logs) != 1 {
		t.Fatalf("body = %s, want one envelope with one log", body)
	}
	return envelopes[0].Logs[0].Attributes
}

// TestNewRelic_Options proves every option reaches the sender and New wraps it.
func TestNewRelic_Options(t *testing.T) {
	srv := newFake(t)
	drain, err := newrelic.New(
		newrelic.WithLicenseKey("key"),
		newrelic.WithRegion("eu"),
		newrelic.WithEndpoint(srv.URL),
		newrelic.WithHTTPClient(&http.Client{}),
		newrelic.WithTimeout(2*time.Second),
		newrelic.WithUserAgent("agent"),
		newrelic.WithGzip(true),
		newrelic.WithPipeline(pipeline.BatchSize(1)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drain.Send(context.Background(), requestEvent())
	flusher, ok := drain.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("the wrapped drain has no Flush")
	}
	if err := flusher.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if srv.last() == "" {
		t.Error("no request arrived")
	}
}

// TestNewRelic_Env proves the environment supplies the key and the region.
func TestNewRelic_Env(t *testing.T) {
	t.Setenv("NEW_RELIC_LICENSE_KEY", "envkey")
	t.Setenv("NEW_RELIC_REGION", "jp")
	if _, err := newrelic.NewSender(); err != nil {
		t.Fatalf("NewSender: %v", err)
	}
}

// TestNewRelic_MustNewPanics proves a missing key panics rather than returning nil.
func TestNewRelic_MustNewPanics(t *testing.T) {
	t.Setenv("NEW_RELIC_LICENSE_KEY", "")
	t.Setenv("NEW_RELIC_API_KEY", "")
	defer func() {
		if recover() == nil {
			t.Error("MustNew did not panic on a missing key")
		}
	}()
	newrelic.MustNew()
}

// TestNewRelic_Regions proves each region name is accepted and an unknown one is refused.
func TestNewRelic_Regions(t *testing.T) {
	for _, region := range []string{"us", "eu", "jp", "fedramp", ""} {
		if _, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithRegion(region)); err != nil {
			t.Errorf("region %q: %v", region, err)
		}
	}
	if _, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithRegion("nope")); err == nil {
		t.Error("NewSender accepted an unknown region")
	}
}

// TestNewRelic_NoSummary proves an event with no summary sends an empty message and a
// fresh timestamp.
func TestNewRelic_NoSummary(t *testing.T) {
	srv := newFake(t)
	sender, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{{"kind": "job"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	var envelopes []struct {
		Logs []struct {
			Timestamp int64  `json:"timestamp"`
			Message   string `json:"message"`
		} `json:"logs"`
	}
	if err := json.Unmarshal([]byte(srv.last()), &envelopes); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	log := envelopes[0].Logs[0]
	if log.Message != "" {
		t.Errorf("message = %q, want empty", log.Message)
	}
	if log.Timestamp == 0 {
		t.Error("timestamp = 0, want the current time")
	}
}
