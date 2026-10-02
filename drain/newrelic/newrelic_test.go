package newrelic_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/newrelic"
	"github.com/jeremygprawira/wlog/internal/httpfake"
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
		body := httpfake.Body(r)
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

// TestNewRelic_P1_PostsToTheLogV1Path proves the drain posts to the Log API path. Before,
// every region and WithEndpoint posted to the host root, so no event could arrive.
func TestNewRelic_P1_PostsToTheLogV1Path(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()

	sender, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	if req == nil {
		t.Fatal("no request recorded")
	}
	if req.Path != "/log/v1" {
		t.Errorf("path = %q, want /log/v1", req.Path)
	}
	if got := req.Headers.Get("Api-Key"); got != "key" {
		t.Errorf("Api-Key = %q, want key", got)
	}
}

// TestNewRelic_P4_SplitsARequestOverTheLimit proves the drain splits a batch at the
// 1,000,000 byte Log API limit, so one large request does not lose the batch.
func TestNewRelic_P4_SplitsARequestOverTheLimit(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()

	sender, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	big := strings.Repeat("x", 64*1024)
	var events []map[string]any
	for i := 0; i < 40; i++ {
		events = append(events, map[string]any{
			"summary": "GET /orders",
			"level":   "info",
			"service": map[string]any{"name": "checkout"},
			"blob":    big,
		})
	}
	if err := sender.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	requests := srv.Requests()
	if len(requests) < 2 {
		t.Errorf("requests = %d, want more than one for a body over 1 MB", len(requests))
	}
	for _, req := range requests {
		if len(req.Body) > 1_000_000 {
			t.Errorf("a request held %d bytes, want at most 1,000,000", len(req.Body))
		}
	}
}

// TestNewRelic_P5_StatusTable proves the whole-request statuses classify as the spec says.
func TestNewRelic_P5_StatusTable(t *testing.T) {
	for _, tc := range []struct {
		status    int
		retryable bool
		ok        bool
	}{
		{http.StatusAccepted, false, true},
		{http.StatusBadRequest, false, false},
		{http.StatusUnauthorized, false, false},
		{http.StatusForbidden, false, false},
		{http.StatusRequestTimeout, true, false},
		{http.StatusRequestEntityTooLarge, false, false},
		{http.StatusTooManyRequests, true, false},
		{http.StatusServiceUnavailable, true, false},
	} {
		srv := httpfake.New()
		srv.SetStatus(tc.status)
		sender, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL))
		if err != nil {
			srv.Close()
			t.Fatalf("NewSender: %v", err)
		}
		err = sender.SendBatch(context.Background(), []map[string]any{requestEvent()})
		srv.Close()
		if tc.ok {
			if err != nil {
				t.Errorf("status %d: SendBatch = %v, want nil", tc.status, err)
			}
			continue
		}
		var re interface{ Retryable() bool }
		if !errors.As(err, &re) {
			t.Errorf("status %d: error %v is not a RetryError", tc.status, err)
			continue
		}
		if re.Retryable() != tc.retryable {
			t.Errorf("status %d: retryable = %v, want %v", tc.status, re.Retryable(), tc.retryable)
		}
	}
}

// TestNewRelic_P6_GzipIsOnByDefault proves the drain compresses by default, and that
// WithGzip(false) turns it off.
func TestNewRelic_P6_GzipIsOnByDefault(t *testing.T) {
	for _, tc := range []struct {
		name     string
		opts     []newrelic.Option
		encoding string
	}{
		{"default", nil, "gzip"},
		{"off", []newrelic.Option{newrelic.WithGzip(false)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httpfake.New()
			defer srv.Close()
			opts := append([]newrelic.Option{
				newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL),
			}, tc.opts...)
			sender, err := newrelic.NewSender(opts...)
			if err != nil {
				t.Fatalf("NewSender: %v", err)
			}
			if err := sender.SendBatch(context.Background(), []map[string]any{requestEvent()}); err != nil {
				t.Fatalf("SendBatch: %v", err)
			}
			req := srv.Last()
			if req == nil {
				t.Fatal("no request recorded")
			}
			if got := req.Headers.Get("Content-Encoding"); got != tc.encoding {
				t.Errorf("Content-Encoding = %q, want %q", got, tc.encoding)
			}
		})
	}
}

// TestNewRelic_P9_CapKeepsReservedKeys proves the attribute cap keeps the reserved keys, so
// a trace id and a service name survive a wide event.
func TestNewRelic_P9_CapKeepsReservedKeys(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	sender, err := newrelic.NewSender(newrelic.WithLicenseKey("key"), newrelic.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	event := map[string]any{
		"level":     "info",
		"operation": "GET /orders",
		"kind":      "request",
		"service":   map[string]any{"name": "checkout"},
		"trace":     map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"},
	}
	for i := 0; i < 300; i++ {
		event[fmt.Sprintf("a%04d", i)] = i
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	req := srv.Last()
	if req == nil {
		t.Fatal("no request recorded")
	}
	var envelope []struct {
		Logs []struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(req.Body, &envelope); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	attributes := envelope[0].Logs[0].Attributes
	for _, key := range []string{"trace.id", "service.name", "level", "operation"} {
		if _, ok := attributes[key]; !ok {
			t.Errorf("the capped log lost %s", key)
		}
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
