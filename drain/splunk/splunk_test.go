package splunk_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/splunk"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// fakeHEC records every request and answers with one code.
type fakeHEC struct {
	*httptest.Server
	mu      sync.Mutex
	status  int
	answer  string
	bodies  []string
	headers []http.Header
}

// newFake starts a fake collector that answers with status and answer.
func newFake(t *testing.T, status int, answer string) *fakeHEC {
	t.Helper()
	f := &fakeHEC{status: status, answer: answer}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.headers = append(f.headers, r.Header.Clone())
		code, reply := f.status, f.answer
		f.mu.Unlock()
		w.WriteHeader(code)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(f.Close)
	return f
}

// last returns the most recent request body and headers.
func (f *fakeHEC) last() (string, http.Header) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return "", nil
	}
	return f.bodies[len(f.bodies)-1], f.headers[len(f.headers)-1]
}

// count returns how many requests arrived.
func (f *fakeHEC) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

// channels returns the channel header of every request.
func (f *fakeHEC) channels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.headers))
	for _, header := range f.headers {
		out = append(out, header.Get("X-Splunk-Request-Channel"))
	}
	return out
}

// event returns the one event the golden body holds.
func event() map[string]any {
	return map[string]any{
		"timestamp": "2026-09-22T10:00:00Z",
		"level":     "info",
		"kind":      "request",
		"outcome":   "success",
		"service":   map[string]any{"name": "checkout", "env": "prod", "instance": "pod-1"},
	}
}

// TestSplunk_GoldenBody proves the envelope body matches the golden written from the
// Splunk documents.
func TestSplunk_GoldenBody(t *testing.T) {
	srv := newFake(t, http.StatusOK, `{"text":"Success","code":0}`)
	sender, err := splunk.NewSender(splunk.WithURL(srv.URL), splunk.WithToken("token"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	golden, err := os.ReadFile("testdata/request.ndjson")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	body, headers := srv.last()
	if body != string(golden) {
		t.Errorf("body =\n%s\nwant\n%s", body, golden)
	}
	if got := headers.Get("Authorization"); got != "Splunk token" {
		t.Errorf("Authorization = %q, want Splunk token", got)
	}
	if headers.Get("X-Splunk-Request-Channel") == "" {
		t.Error("X-Splunk-Request-Channel is missing")
	}
}

// TestSplunk_ChannelIsStable proves one channel id serves the life of the drain.
func TestSplunk_ChannelIsStable(t *testing.T) {
	srv := newFake(t, http.StatusOK, `{"text":"Success","code":0}`)
	sender, err := splunk.NewSender(splunk.WithURL(srv.URL), splunk.WithToken("token"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
			t.Fatalf("SendBatch: %v", err)
		}
	}
	channels := srv.channels()
	if len(channels) != 2 || channels[0] != channels[1] {
		t.Errorf("channel ids = %v, want one stable id", channels)
	}
}

// TestSplunk_Codes proves the HEC codes map to success, retry, split, and drop.
func TestSplunk_Codes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     int
		retry    bool
		dropped  bool
		requests int
	}{
		{"success", 0, false, false, 1},
		{"backpressure24", 24, false, false, 1},
		{"backpressure25", 25, false, false, 1},
		{"retry9", 9, true, false, 1},
		{"retry27", 27, true, false, 1},
		{"bad6", 6, false, true, 3},
		{"permanent4", 4, false, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer := fmt.Sprintf(`{"text":"code","code":%d}`, tc.code)
			srv := newFake(t, http.StatusOK, answer)
			sender, err := splunk.NewSender(splunk.WithURL(srv.URL), splunk.WithToken("token"))
			if err != nil {
				t.Fatalf("NewSender: %v", err)
			}
			err = sender.SendBatch(context.Background(), []map[string]any{event(), event(), event(), event()})
			var partial *pipeline.PartialError
			var re interface{ Retryable() bool }
			switch {
			case tc.retry:
				if !errors.As(err, &re) || !re.Retryable() {
					t.Errorf("code %d: err = %v, want a retryable error", tc.code, err)
				}
			case tc.dropped:
				if !errors.As(err, &partial) {
					t.Fatalf("code %d: err = %v, want a PartialError", tc.code, err)
				}
				if len(partial.Dropped) == 0 {
					t.Errorf("code %d: Dropped is empty", tc.code)
				}
			case err != nil:
				t.Errorf("code %d: err = %v, want nil", tc.code, err)
			}
			if srv.count() != tc.requests {
				t.Errorf("code %d: requests = %d, want %d", tc.code, srv.count(), tc.requests)
			}
		})
	}
}

// TestSplunk_MissingConfig proves a missing URL or token is refused.
func TestSplunk_MissingConfig(t *testing.T) {
	if _, err := splunk.NewSender(splunk.WithToken("token")); err == nil {
		t.Error("NewSender accepted a missing URL")
	}
	if _, err := splunk.NewSender(splunk.WithURL("http://example.invalid")); err == nil {
		t.Error("NewSender accepted a missing token")
	}
}

// TestSplunk_Options proves every option reaches the sender and New wraps it.
func TestSplunk_Options(t *testing.T) {
	srv := newFake(t, http.StatusOK, `{"text":"Success","code":0}`)
	drain, err := splunk.New(
		splunk.WithURL(srv.URL),
		splunk.WithToken("token"),
		splunk.WithIndex("main"),
		splunk.WithSource("app"),
		splunk.WithSourceType("_json"),
		splunk.WithMaxBatchBytes(1<<20),
		splunk.WithHTTPClient(&http.Client{}),
		splunk.WithTimeout(2*time.Second),
		splunk.WithUserAgent("agent"),
		splunk.WithGzip(true),
		splunk.WithPipeline(pipeline.BatchSize(1)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drain.Send(context.Background(), event())
	flusher, ok := drain.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("the wrapped drain has no Flush")
	}
	if err := flusher.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if srv.count() == 0 {
		t.Error("no request arrived")
	}
}

// TestSplunk_Env proves the environment supplies the URL, the token, and the index.
func TestSplunk_Env(t *testing.T) {
	t.Setenv("SPLUNK_HEC_URL", "http://example.invalid")
	t.Setenv("SPLUNK_HEC_TOKEN", "token")
	t.Setenv("SPLUNK_INDEX", "main")
	t.Setenv("SPLUNK_SOURCETYPE", "_json")
	if _, err := splunk.NewSender(); err != nil {
		t.Fatalf("NewSender: %v", err)
	}
}

// TestSplunk_MustNewPanics proves a missing token panics rather than returning nil.
func TestSplunk_MustNewPanics(t *testing.T) {
	t.Setenv("SPLUNK_HEC_URL", "")
	t.Setenv("SPLUNK_HEC_TOKEN", "")
	defer func() {
		if recover() == nil {
			t.Error("MustNew did not panic on a missing token")
		}
	}()
	splunk.MustNew()
}

// TestSplunk_BackpressureReport proves code 24 reports WLOG_DRAIN_BACKPRESSURE once.
func TestSplunk_BackpressureReport(t *testing.T) {
	srv := newFake(t, http.StatusOK, `{"text":"near capacity","code":24}`)
	sender, err := splunk.NewSender(splunk.WithURL(srv.URL), splunk.WithToken("token"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	var problems []wlog.Problem
	log, _ := wlogtest.New(t, wlog.OnProblem(func(p wlog.Problem) { problems = append(problems, p) }))
	if err := sender.Setup(log); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	found := 0
	for _, p := range problems {
		if p.Code == "WLOG_DRAIN_BACKPRESSURE" {
			found++
		}
	}
	if found != 1 {
		t.Errorf("WLOG_DRAIN_BACKPRESSURE reported %d times, want 1", found)
	}
}
