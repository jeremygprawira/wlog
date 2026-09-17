// Package webhook posts wlog events to any HTTP URL as a JSON array, or as NDJSON. It
// can sign the body with an HMAC-SHA256 header. It reads WLOG_WEBHOOK_URL when
// WithURL is not given.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// config holds the resolved configuration for one Drain.
type config struct {
	url     string
	headers map[string]string
	ndjson  bool
	secret  string
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithURL sets the target URL. Overrides WLOG_WEBHOOK_URL.
func WithURL(url string) Option { return func(c *config) { c.url = url } }

// WithHeaders sets extra headers on every request.
func WithHeaders(headers map[string]string) Option {
	return func(c *config) {
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

// WithNDJSON sends one JSON event per line instead of one JSON array. Off by default.
func WithNDJSON(on bool) Option { return func(c *config) { c.ndjson = on } }

// WithSecret signs every body with HMAC-SHA256 and sets X-Wlog-Signature.
func WithSecret(secret string) Option { return func(c *config) { c.secret = secret } }

// Drain posts batches to one URL. It implements pipeline.Sender, so wrap it with
// pipeline.Wrap to get batching, retry, and a bounded buffer.
type Drain struct {
	client *httpdrain.Client
	ndjson bool
}

// New builds a Drain from opts and WLOG_WEBHOOK_URL. It returns an error when the URL
// is missing.
func New(opts ...Option) (*Drain, error) {
	c := config{
		url:     os.Getenv("WLOG_WEBHOOK_URL"),
		headers: map[string]string{},
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.url == "" {
		return nil, fmt.Errorf("webhook: WLOG_WEBHOOK_URL is required")
	}

	clientOpts := []httpdrain.Option{httpdrain.WithSource("webhook")}
	for k, v := range c.headers {
		clientOpts = append(clientOpts, httpdrain.WithHeader(k, v))
	}
	if c.secret != "" {
		clientOpts = append(clientOpts, httpdrain.WithHeaderFunc(signatureHeader(c.secret)))
	}
	return &Drain{client: httpdrain.New(c.url, clientOpts...), ndjson: c.ndjson}, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) *Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// signatureHeader returns the header function for one secret. The signature covers the
// uncompressed body, so it stays valid when gzip is also on.
func signatureHeader(secret string) func([]byte) map[string]string {
	return func(body []byte) map[string]string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		return map[string]string{"X-Wlog-Signature": "sha256=" + hex.EncodeToString(mac.Sum(nil))}
	}
}

// SendBatch posts the events as one JSON array, or as NDJSON when WithNDJSON is on.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
	if d.ndjson {
		var buf strings.Builder
		for _, event := range events {
			line, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("webhook: marshal event: %w", err)
			}
			buf.Write(line)
			buf.WriteByte('\n')
		}
		return d.client.Post(ctx, []byte(buf.String()), "application/x-ndjson")
	}

	body, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("webhook: marshal events: %w", err)
	}
	return d.client.Post(ctx, body, "application/json")
}
