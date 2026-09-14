// Package httpdrain is the shared HTTP-POST helper every phase-4 drain (Axiom, Loki,
// the generic webhook, OTLP) builds on: identity headers, optional gzip, and a single
// place that turns an HTTP response into a retryable-or-not error for pipeline.Wrap.
package httpdrain

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jeremygprawira/wlog/internal/version"
)

// Client POSTs batches to one URL with wlog's identity headers.
type Client struct {
	url        string
	httpClient *http.Client
	headers    map[string]string
	gzip       bool
	source     string
	userAgent  string
}

// Option configures a Client built by New.
type Option func(*Client)

// New builds a Client posting to url. Default: 10s timeout, User-Agent
// "wlog/<version>", no X-Wlog-Source, no gzip.
func New(url string, opts ...Option) *Client {
	c := &Client{
		url:        url,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		headers:    map[string]string{},
		userAgent:  "wlog/" + version.Version,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithTimeout sets the HTTP request timeout. Default 10s.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// WithHeader sets one extra header on every request (e.g. an API key).
func WithHeader(key, value string) Option {
	return func(c *Client) { c.headers[key] = value }
}

// WithGzip compresses the body and sets Content-Encoding: gzip when on.
func WithGzip(on bool) Option { return func(c *Client) { c.gzip = on } }

// WithSource sets X-Wlog-Source (e.g. "axiom", "loki"). Empty (the default) omits
// the header.
func WithSource(name string) Option { return func(c *Client) { c.source = name } }

// WithUserAgent overrides the User-Agent header; an empty string omits it entirely.
// Default "wlog/<version>".
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// Post sends body as one request. A 2xx response returns nil; anything else returns
// a *StatusError classifying whether it is worth retrying and, for a 429/503 with a
// Retry-After header, how long to wait.
func (c *Client) Post(ctx context.Context, body []byte, contentType string) error {
	payload := body
	encoding := ""
	if c.gzip {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(body); err != nil {
			return fmt.Errorf("httpdrain: gzip: %w", err)
		}
		if err := zw.Close(); err != nil {
			return fmt.Errorf("httpdrain: gzip: %w", err)
		}
		payload = buf.Bytes()
		encoding = "gzip"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("httpdrain: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.source != "" {
		req.Header.Set("X-Wlog-Source", c.source)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("httpdrain: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return newStatusError(resp)
}

// StatusError is returned by Post for any non-2xx response.
type StatusError struct {
	Status     int
	retryable  bool
	retryAfter time.Duration
}

func newStatusError(resp *http.Response) *StatusError {
	retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	return &StatusError{
		Status:     resp.StatusCode,
		retryable:  retryable,
		retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("httpdrain: unexpected status %d", e.Status)
}

// Retryable reports whether the error is worth retrying: 429 and 5xx are, 4xx
// otherwise is not (the request itself was rejected, so retrying it verbatim would
// just fail again).
func (e *StatusError) Retryable() bool { return e.retryable }

// RetryAfter is how long to wait before retrying, from a Retry-After response header
// (seconds form). Zero means "no server-specified delay; use the normal backoff".
func (e *StatusError) RetryAfter() time.Duration { return e.retryAfter }

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
