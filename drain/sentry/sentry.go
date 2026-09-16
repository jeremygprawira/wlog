// Package sentry sends wlog error events to Sentry as envelope items, grouped into one
// issue per error code, with the whole wide event attached as context. It reads
// SENTRY_DSN when WithDSN is not given.
package sentry

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jeremygprawira/wlog/internal/httpdrain"
	"github.com/jeremygprawira/wlog/internal/version"
)

// contentType is the Sentry envelope content type.
const contentType = "application/x-sentry-envelope"

// config holds the resolved configuration for one Drain.
type config struct {
	dsn       string
	allEvents bool
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithDSN sets the Sentry DSN. Overrides SENTRY_DSN.
func WithDSN(dsn string) Option { return func(c *config) { c.dsn = dsn } }

// WithAllEvents also sends every non-error event as a Sentry log item. Off by default.
// Overrides SENTRY_ALL_EVENTS.
func WithAllEvents(on bool) Option { return func(c *config) { c.allEvents = on } }

// Drain sends batches to the Sentry envelope API. It implements pipeline.Sender, so wrap
// it with pipeline.Wrap to get batching, retry, and a bounded buffer.
type Drain struct {
	client    *httpdrain.Client
	allEvents bool
}

// New builds a Drain from opts and SENTRY_DSN. It returns an error when the DSN is
// missing or malformed.
func New(opts ...Option) (*Drain, error) {
	c := config{
		dsn:       os.Getenv("SENTRY_DSN"),
		allEvents: envBool("SENTRY_ALL_EVENTS"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	endpoint, publicKey, err := parseDSN(c.dsn)
	if err != nil {
		return nil, err
	}
	client := httpdrain.New(endpoint,
		httpdrain.WithSource("sentry"),
		httpdrain.WithHeader("X-Sentry-Auth",
			"Sentry sentry_version=7, sentry_key="+publicKey+", sentry_client="+version.UserAgent()),
	)
	return &Drain{client: client, allEvents: c.allEvents}, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) *Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// parseDSN turns a Sentry DSN into the envelope endpoint and the public key. The DSN
// shape is https://<public_key>@<host>/<project_id>.
func parseDSN(dsn string) (endpoint, publicKey string, err error) {
	if dsn == "" {
		return "", "", errors.New("sentry: SENTRY_DSN is required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", "", fmt.Errorf("sentry: parse DSN: %w", err)
	}
	if parsed.User != nil {
		publicKey = parsed.User.Username()
	}
	if publicKey == "" {
		return "", "", errors.New("sentry: DSN is missing the public key")
	}
	project := strings.Trim(parsed.Path, "/")
	if project == "" {
		return "", "", errors.New("sentry: DSN is missing the project id")
	}
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "https"
	}
	if parsed.Host == "" {
		return "", "", errors.New("sentry: DSN is missing the host")
	}
	return fmt.Sprintf("%s://%s/api/%s/envelope/", scheme, parsed.Host, project), publicKey, nil
}

// envBool reads an env var as a boolean flag. "1" and "true" are true.
func envBool(name string) bool {
	switch strings.ToLower(os.Getenv(name)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// SendBatch builds one envelope. A batch with no error event and AllEvents off sends
// nothing and returns nil.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
	body, err := buildEnvelope(events, d.allEvents)
	if err != nil {
		return err
	}
	if body == nil {
		return nil
	}
	return d.client.Post(ctx, body, contentType)
}
