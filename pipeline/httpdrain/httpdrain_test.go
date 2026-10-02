package httpdrain_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/internal/version"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

func TestHTTPDrain_Post_Success(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()

	c := httpdrain.New(srv.URL, httpdrain.WithSource("axiom"))
	err := c.Post(context.Background(), []byte(`{"a":1}`), "application/json")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	req := srv.Last()
	if req == nil {
		t.Fatal("no request recorded")
	}
	if req.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", req.Headers.Get("Content-Type"))
	}
	if got, want := req.Headers.Get("User-Agent"), version.UserAgent(); got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
	if req.Headers.Get("X-Wlog-Source") != "axiom" {
		t.Errorf("X-Wlog-Source = %q, want axiom", req.Headers.Get("X-Wlog-Source"))
	}
	if string(req.Body) != `{"a":1}` {
		t.Errorf("body = %q", req.Body)
	}
}

// TestHTTPDrain_PostFor_ReturnsBody proves PostFor hands the response body back, so a
// drain that reads one result per event can map it.
func TestHTTPDrain_PostFor_ReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"status":202}]`))
	}))
	defer srv.Close()

	client := httpdrain.New(srv.URL)
	body, err := client.PostFor(context.Background(), []byte(`{}`), "application/json")
	if err != nil {
		t.Fatalf("PostFor: %v", err)
	}
	if string(body) != `[{"status":202}]` {
		t.Errorf("body = %q, want the response body", body)
	}
}

// TestHTTPDrain_P10_PostIgnoresABroken2xxBody proves that Post returns nil for a 2xx
// answer even when the response body cannot be read. Post never wants the body, so a
// broken one must not turn an accepted batch into a failure the pipeline retries.
func TestHTTPDrain_P10_PostIgnoresABroken2xxBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short"))
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()

	client := httpdrain.New(srv.URL)
	if err := client.Post(context.Background(), []byte(`{}`), "application/json"); err != nil {
		t.Errorf("Post = %v, want nil for a 2xx answer", err)
	}
}

// TestHTTPDrain_P4_ChunksSplitsByCountAndBytes proves the shared split cuts a batch at
// the backend's item and byte limits, and that an event which alone passes the byte limit
// is named as oversize instead of being sent.
func TestHTTPDrain_P4_ChunksSplitsByCountAndBytes(t *testing.T) {
	events := []map[string]any{{"i": 0}, {"i": 1}, {"i": 2}, {"i": 3}, {"i": 4}}
	size := func(map[string]any) int { return 10 }

	chunks, oversize := httpdrain.Chunks(events, 2, 1000, size)
	if len(chunks) != 3 {
		t.Errorf("chunks = %d, want 3 at two items each", len(chunks))
	}
	if len(oversize) != 0 {
		t.Errorf("oversize = %v, want none", oversize)
	}
	if chunks[0].End-chunks[0].Start != 2 || chunks[2].End-chunks[2].Start != 1 {
		t.Errorf("chunk sizes = %d and %d, want 2 and 1",
			chunks[0].End-chunks[0].Start, chunks[2].End-chunks[2].Start)
	}

	chunks, oversize = httpdrain.Chunks(events, 0, 25, size)
	if len(chunks) != 3 {
		t.Errorf("chunks = %d, want 3 at two events per byte limit", len(chunks))
	}
	if len(oversize) != 0 {
		t.Errorf("oversize = %v, want none", oversize)
	}

	chunks, oversize = httpdrain.Chunks([]map[string]any{{"big": true}}, 0, 5, func(map[string]any) int { return 10 })
	if len(chunks) != 0 {
		t.Errorf("chunks = %d, want none for one oversize event", len(chunks))
	}
	if len(oversize) != 1 || oversize[0] != 0 {
		t.Errorf("oversize = %v, want the event at index 0", oversize)
	}
}

// TestHTTPDrain_P4_SendChunkHalvesOn413 proves a 413 halves the chunk until each request
// fits, and that one event no request can carry is dropped with reason too_large.
func TestHTTPDrain_P4_SendChunkHalvesOn413(t *testing.T) {
	events := []map[string]any{{"i": 0}, {"i": 1}, {"i": 2}, {"i": 3}}
	posts := 0
	post := func(_ context.Context, chunk []map[string]any) error {
		posts++
		if len(chunk) > 2 {
			return &httpdrain.StatusError{Status: http.StatusRequestEntityTooLarge}
		}
		return nil
	}
	pe, err := httpdrain.SendChunk(context.Background(), events, httpdrain.Chunk{Start: 0, End: len(events)}, post)
	if err != nil {
		t.Fatalf("SendChunk: %v", err)
	}
	if pe != nil {
		t.Errorf("SendChunk = %v, want nil when every half lands", pe)
	}
	if posts != 3 {
		t.Errorf("posts = %d, want 3: one refusal and two halves", posts)
	}

	always := func(context.Context, []map[string]any) error {
		return &httpdrain.StatusError{Status: http.StatusRequestEntityTooLarge}
	}
	pe, err = httpdrain.SendChunk(context.Background(), events[:1], httpdrain.Chunk{Start: 0, End: 1}, always)
	if err != nil {
		t.Fatalf("SendChunk: %v", err)
	}
	if pe == nil {
		t.Fatal("SendChunk = nil, want the event dropped as too large")
	}
	if len(pe.Dropped) != 1 || pe.Dropped[0] != 0 {
		t.Errorf("Dropped = %v, want the event at index 0", pe.Dropped)
	}
	if pe.Reason != "too_large" {
		t.Errorf("Reason = %q, want too_large", pe.Reason)
	}
}

// TestHTTPDrain_D1_Non2xxReturnsTheBody proves PostFor returns the capped response body
// next to the StatusError, so a backend that explains a refusal in the body, such as
// Splunk with its HEC code, is readable.
func TestHTTPDrain_D1_Non2xxReturnsTheBody(t *testing.T) {
	answer := `{"code":6,"text":"Invalid data format"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()

	client := httpdrain.New(srv.URL)
	body, err := client.PostFor(context.Background(), []byte(`{}`), "application/json")
	var se *httpdrain.StatusError
	if !asStatusError(err, &se) {
		t.Fatalf("err = %v, want a *StatusError", err)
	}
	if string(se.Body) != answer {
		t.Errorf("StatusError.Body = %q, want the answer", se.Body)
	}
	if string(body) != answer {
		t.Errorf("body = %q, want the same answer", body)
	}
}

// TestHTTPDrain_P13_ResponseCap proves PostFor returns at most 4 MiB of a response body, so
// a backend that streams an endless answer cannot grow memory without bound.
func TestHTTPDrain_P13_ResponseCap(t *testing.T) {
	const cap = 4 << 20
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), cap+1024))
	}))
	defer srv.Close()

	client := httpdrain.New(srv.URL)
	body, err := client.PostFor(context.Background(), []byte(`{}`), "application/json")
	if err != nil {
		t.Fatalf("PostFor: %v", err)
	}
	if len(body) != cap {
		t.Errorf("body = %d bytes, want the %d byte cap", len(body), cap)
	}
}

func TestHTTPDrain_CustomHeaders(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	c := httpdrain.New(srv.URL, httpdrain.WithHeader("Authorization", "Bearer tok"))
	_ = c.Post(context.Background(), []byte("x"), "text/plain")

	if srv.Last().Headers.Get("Authorization") != "Bearer tok" {
		t.Errorf("Authorization header missing/wrong: %v", srv.Last().Headers)
	}
}

func TestHTTPDrain_HeaderFunc(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()

	c := httpdrain.New(srv.URL, httpdrain.WithHeaderFunc(func(body []byte) map[string]string {
		return map[string]string{"X-Body-Length": strconv.Itoa(len(body))}
	}))
	if err := c.Post(context.Background(), []byte("hello"), "text/plain"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if got := srv.Last().Headers.Get("X-Body-Length"); got != "5" {
		t.Errorf("X-Body-Length = %q, want 5", got)
	}
}

func TestHTTPDrain_IdentityHeaders_Disable(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()

	c := httpdrain.New(srv.URL, httpdrain.WithUserAgent(""), httpdrain.WithSource(""))
	_ = c.Post(context.Background(), []byte("x"), "text/plain")

	req := srv.Last()
	if req.Headers.Get("User-Agent") != "" && req.Headers.Get("User-Agent") != "Go-http-client/1.1" {
		t.Errorf("User-Agent not disabled: %q", req.Headers.Get("User-Agent"))
	}
	if req.Headers.Get("X-Wlog-Source") != "" {
		t.Errorf("X-Wlog-Source not disabled: %q", req.Headers.Get("X-Wlog-Source"))
	}
}

func TestHTTPDrain_Gzip(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()

	c := httpdrain.New(srv.URL, httpdrain.WithGzip(true))
	if err := c.Post(context.Background(), []byte(`{"a":1}`), "application/json"); err != nil {
		t.Fatalf("Post: %v", err)
	}

	req := srv.Last()
	if req.Headers.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", req.Headers.Get("Content-Encoding"))
	}
	// The fake decodes a gzip body, as a backend does, so the recorded bytes are the
	// plain ones and a body that is not valid gzip stays encoded and fails here.
	if string(req.Body) != `{"a":1}` {
		t.Errorf("body = %q, want the decoded request body", req.Body)
	}
}

func TestHTTPDrain_StatusClassification(t *testing.T) {
	cases := []struct {
		status       int
		wantRetry    bool
		wantIsStatus bool
	}{
		{200, false, false},
		{429, true, true},
		{500, true, true},
		{503, true, true},
		{400, false, true},
		{401, false, true},
	}
	for _, tc := range cases {
		srv := httpfake.New()
		srv.SetStatus(tc.status)

		c := httpdrain.New(srv.URL)
		err := c.Post(context.Background(), []byte("x"), "text/plain")
		srv.Close()

		if tc.status == 200 {
			if err != nil {
				t.Errorf("status 200: err = %v, want nil", err)
			}
			continue
		}
		var se *httpdrain.StatusError
		if !asStatusError(err, &se) {
			t.Errorf("status %d: err is not a *StatusError: %v", tc.status, err)
			continue
		}
		if se.Retryable() != tc.wantRetry {
			t.Errorf("status %d: Retryable() = %v, want %v", tc.status, se.Retryable(), tc.wantRetry)
		}
	}
}

func TestHTTPDrain_RetryAfterHonoured(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	srv.SetStatus(429)
	srv.SetHeader("Retry-After", "7")

	c := httpdrain.New(srv.URL)
	err := c.Post(context.Background(), []byte("x"), "text/plain")

	var se *httpdrain.StatusError
	if !asStatusError(err, &se) {
		t.Fatalf("err is not a *StatusError: %v", err)
	}
	if se.RetryAfter() != 7*time.Second {
		t.Errorf("RetryAfter() = %v, want 7s", se.RetryAfter())
	}
}

func asStatusError(err error, target **httpdrain.StatusError) bool {
	var se *httpdrain.StatusError
	if errors.As(err, &se) {
		*target = se
		return true
	}
	return false
}

// TestHTTPDrain_PIPE19_ErrorHasNoSecrets proves that a transport error never carries
// the query string or the user info of the drain URL (gate G1). A token in a query,
// such as ?token=..., reaches OnDropped through this error, so it must not be there.
func TestHTTPDrain_PIPE19_ErrorHasNoSecrets(t *testing.T) {
	// Port 1 has no listener, so the request fails at the transport layer.
	target := "http://user:s3cr3t@127.0.0.1:1/ingest?token=s3cr3t&api_key=abc"
	c := httpdrain.New(target, httpdrain.WithTimeout(100*time.Millisecond))

	err := c.Post(context.Background(), []byte("x"), "application/json")
	if err == nil {
		t.Fatal("Post to a dead listener returned nil")
	}
	msg := err.Error()
	for _, secret := range []string{"s3cr3t", "user:", "token=", "api_key=", "?"} {
		if strings.Contains(msg, secret) {
			t.Errorf("error message leaked %q: %s", secret, msg)
		}
	}
}

// countingTransport records that the drain used the client it was given.
type countingTransport struct {
	calls int32
}

// RoundTrip counts the call and forwards to the default transport.
func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&t.calls, 1)
	return http.DefaultTransport.RoundTrip(req)
}

// TestHTTPDrain_PIPE24_ClientOptions proves that a drain can supply its own HTTP
// client, timeout, and user agent, and that wlog never changes the caller's client.
func TestHTTPDrain_PIPE24_ClientOptions(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()

	transport := &countingTransport{}
	custom := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	c := httpdrain.New(srv.URL,
		httpdrain.WithHTTPClient(custom),
		httpdrain.WithTimeout(2*time.Second),
		httpdrain.WithUserAgent("my-agent/1"),
	)
	if err := c.Post(context.Background(), []byte("x"), "application/json"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if got := atomic.LoadInt32(&transport.calls); got != 1 {
		t.Errorf("custom transport saw %d calls, want 1", got)
	}
	if custom.Timeout != 30*time.Second {
		t.Errorf("WithTimeout changed the caller's client timeout to %v", custom.Timeout)
	}
	if got := srv.Last().Headers.Get("User-Agent"); got != "my-agent/1" {
		t.Errorf("User-Agent = %q, want my-agent/1", got)
	}
}

// TestHTTPDrain_PIPE24_TimeoutApplies proves the timeout reaches the request even when
// the caller supplied the client.
func TestHTTPDrain_PIPE24_TimeoutApplies(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	c := httpdrain.New(slow.URL,
		httpdrain.WithHTTPClient(&http.Client{Timeout: time.Minute}),
		httpdrain.WithTimeout(20*time.Millisecond),
	)
	err := c.Post(context.Background(), []byte("x"), "application/json")
	if err == nil {
		t.Fatal("Post ignored the configured timeout")
	}
	// net/http reports its own Timeout as a plain net.Error, not a wrapped
	// context.DeadlineExceeded. Go 1.26's version of that type also satisfies
	// errors.Is(err, context.DeadlineExceeded), but Go 1.21's does not, so this
	// test checks the one thing both floors guarantee: Timeout() is true.
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("err = %v, want a net.Error with Timeout() true", err)
	}
}

// TestHTTPDrain_PAR18_HeadersOff proves both identity knobs: an empty user agent sends
// none, and WithIdentityHeaders(false) sends neither the wlog user agent nor the wlog
// source header, while a user agent the caller chose itself still goes.
func TestHTTPDrain_PAR18_HeadersOff(t *testing.T) {
	srv := httpfake.New()
	defer srv.Close()
	ctx := context.Background()

	// An empty user agent sends none. Go's own default may still appear.
	c := httpdrain.New(srv.URL, httpdrain.WithUserAgent(""))
	if err := c.Post(ctx, []byte("x"), "text/plain"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if ua := srv.Last().Headers.Get("User-Agent"); ua != "" && ua != "Go-http-client/1.1" {
		t.Errorf("User-Agent = %q, want none", ua)
	}

	// With identity headers off, wlog sends neither of its own headers.
	off := httpdrain.New(srv.URL, httpdrain.WithIdentityHeaders(false), httpdrain.WithSource("axiom"))
	if err := off.Post(ctx, []byte("x"), "text/plain"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	req := srv.Last()
	if src := req.Headers.Get("X-Wlog-Source"); src != "" {
		t.Errorf("X-Wlog-Source = %q, want none", src)
	}
	if ua := req.Headers.Get("User-Agent"); ua != "" && ua != "Go-http-client/1.1" {
		t.Errorf("User-Agent = %q, want none", ua)
	}

	// A user agent the caller chose is not a wlog identity header, so it still goes.
	own := httpdrain.New(srv.URL,
		httpdrain.WithIdentityHeaders(false),
		httpdrain.WithUserAgent("checkout-api/2.1"),
	)
	if err := own.Post(ctx, []byte("x"), "text/plain"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	req = srv.Last()
	if got := req.Headers.Get("User-Agent"); got != "checkout-api/2.1" {
		t.Errorf("User-Agent = %q, want the caller's own value", got)
	}
	if src := req.Headers.Get("X-Wlog-Source"); src != "" {
		t.Errorf("X-Wlog-Source = %q, want none", src)
	}
}
