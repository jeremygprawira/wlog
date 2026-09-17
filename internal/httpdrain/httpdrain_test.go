package httpdrain_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/internal/httpdrain"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/internal/version"
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
	zr, err := gzip.NewReader(bytes.NewReader(req.Body))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	decompressed, _ := io.ReadAll(zr)
	if string(decompressed) != `{"a":1}` {
		t.Errorf("decompressed body = %q", decompressed)
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
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}
