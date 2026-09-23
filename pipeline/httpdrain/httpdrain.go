// Package httpdrain is the shared HTTP-POST helper every phase-4 drain (Axiom, Loki,
// the generic webhook, OTLP) builds on: identity headers, optional gzip, and a single
// place that turns an HTTP response into a retryable-or-not error for pipeline.Wrap.
package httpdrain

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog/internal/version"
)

// Client POSTs batches to one URL with wlog's identity headers.
type Client struct {
	url        string
	httpClient *http.Client
	timeout    time.Duration
	timeoutSet bool
	headers    map[string]string
	headerFunc func(body []byte) map[string]string
	gzip       bool
	source     string
	userAgent  string
	// userAgentSet records that the caller chose the user agent, and identity reports
	// whether wlog's own identity headers go.
	userAgentSet bool
	identity     bool
}

// New builds a Client posting to url. Default: 10s timeout, User-Agent
// "wlog/<version>", no X-Wlog-Source, no gzip.
func New(url string, opts ...Option) *Client {
	c := &Client{
		url:        url,
		httpClient: &http.Client{Timeout: defaultTimeout},
		timeout:    defaultTimeout,
		timeoutSet: true,
		headers:    map[string]string{},
		userAgent:  version.UserAgent(),
		identity:   true,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// maxResponseBytes caps a response body that PostFor reads, so a backend that streams
// an endless answer cannot grow memory without bound.
const maxResponseBytes = 4 << 20

// Post sends body as one request and discards the response body. A 2xx response returns
// nil; anything else returns a *StatusError classifying whether it is worth retrying and,
// for a 429/503 with a Retry-After header, how long to wait.
func (c *Client) Post(ctx context.Context, body []byte, contentType string) error {
	_, err := c.PostFor(ctx, body, contentType)
	return err
}

// PostFor sends body as one request and returns the response body, for a backend that
// reports one result per event. A 2xx response returns the body and a nil error;
// anything else returns a *StatusError, and the body stays unread, because an error must
// never carry a response body. The body is capped at maxResponseBytes.
func (c *Client) PostFor(ctx context.Context, body []byte, contentType string) ([]byte, error) {
	payload := body
	encoding := ""
	if c.gzip {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(body); err != nil {
			return nil, fmt.Errorf("httpdrain: gzip: %w", err)
		}
		if err := zw.Close(); err != nil {
			return nil, fmt.Errorf("httpdrain: gzip: %w", err)
		}
		payload = buf.Bytes()
		encoding = "gzip"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("httpdrain: %w", c.scrubError(err))
	}
	req.Header.Set("Content-Type", contentType)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	if c.userAgent != "" && (c.identity || c.userAgentSet) {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.source != "" && c.identity {
		req.Header.Set("X-Wlog-Source", c.source)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.headerFunc != nil {
		for k, v := range c.headerFunc(body) {
			req.Header.Set(k, v)
		}
	}

	client := c.httpClient
	if c.timeoutSet {
		// The timeout applies to a copy, so a client the caller shares with the
		// rest of the app keeps its own settings.
		cp := *c.httpClient
		cp.Timeout = c.timeout
		client = &cp
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpdrain: %w", c.scrubError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		answer, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("httpdrain: read response: %w", err)
		}
		return answer, nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil, newStatusError(resp)
}

// StatusError is returned by Post for any non-2xx response.
type StatusError struct {
	Status     int
	retryable  bool
	retryAfter time.Duration
}

func newStatusError(resp *http.Response) *StatusError {
	retryable := resp.StatusCode == http.StatusRequestTimeout ||
		resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= 500
	return &StatusError{
		Status:     resp.StatusCode,
		retryable:  retryable,
		retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("httpdrain: unexpected status %d", e.Status)
}

// Retryable reports whether the error is worth retrying: 408, 429, and 5xx are, 4xx
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

// scrubbedError hides the credentials of a drain URL inside a transport error.
//
// It keeps the original error under Unwrap, so errors.Is and errors.As still work.
type scrubbedError struct {
	err     error
	secrets []string
}

// Error returns the original message with every secret replaced.
func (e *scrubbedError) Error() string {
	msg := e.err.Error()
	for _, s := range e.secrets {
		msg = strings.ReplaceAll(msg, s, "REDACTED")
	}
	return msg
}

// Unwrap gives errors.Is and errors.As the original error.
func (e *scrubbedError) Unwrap() error { return e.err }

// scrubError removes the query string and the user info of the drain URL from a
// transport error (gate G1).
//
// net/http builds its error from the full URL, so a token in a query string such as
// ?token=... would otherwise reach OnDropped and any log of the error.
func (c *Client) scrubError(err error) error {
	secrets := c.secrets()
	if len(secrets) == 0 {
		return err
	}
	// A *url.Error carries the URL in its own field, so clean that too.
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = scrubURL(ue.URL)
	}
	return &scrubbedError{err: err, secrets: secrets}
}

// secrets lists the parts of the drain URL that must never appear in an error. The
// longest comes first, so "user:pass" is replaced before "pass" alone.
func (c *Client) secrets() []string {
	u, err := url.Parse(c.url)
	if err != nil {
		return nil
	}
	var out []string
	if u.RawQuery != "" {
		out = append(out, u.RawQuery)
	}
	if u.User != nil {
		if pw, ok := u.User.Password(); ok {
			out = append(out, pw)
		}
		out = append(out, u.User.String())
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// scrubURL returns raw without its user info, query string, or fragment.
func scrubURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
