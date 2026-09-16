package httpdrain_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/internal/httpdrain"
	"github.com/jeremygprawira/wlog/internal/httpfake"
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
	if req.Headers.Get("User-Agent") != "wlog/0.1.0" {
		t.Errorf("User-Agent = %q, want wlog/0.1.0", req.Headers.Get("User-Agent"))
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
	se, ok := err.(*httpdrain.StatusError)
	if ok {
		*target = se
	}
	return ok
}
