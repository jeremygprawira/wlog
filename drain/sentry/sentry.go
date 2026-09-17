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

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/version"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// contentType is the Sentry envelope content type.
const contentType = "application/x-sentry-envelope"

// config holds the resolved configuration for one Sender.
type config struct {
	dsn          string
	allEvents    bool
	pipelineOpts []pipeline.Option
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithDSN sets the Sentry DSN. Overrides SENTRY_DSN.
func WithDSN(dsn string) Option { return func(c *config) { c.dsn = dsn } }

// WithAllEvents also sends every non-error event as a Sentry log item. Off by default.
// Overrides SENTRY_ALL_EVENTS.
func WithAllEvents(on bool) Option { return func(c *config) { c.allEvents = on } }

// WithPipeline sets the pipeline options New wraps the sender with, such as
// pipeline.BatchSize and pipeline.OnDropped. Without it, New uses the pipeline defaults.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// Sender sends batches to the Sentry envelope API. It implements pipeline.Sender.
type Sender struct {
	client    *httpdrain.Client
	allEvents bool
}

// New returns the Sentry drain with the pipeline defaults, or with the options
// WithPipeline set. It returns an error when SENTRY_DSN is missing or malformed.
func New(opts ...Option) (wlog.Drain, error) {
	s, opts2, err := newSender(opts...)
	if err != nil {
		return nil, err
	}
	return pipeline.Wrap(s, opts2...), nil
}

// NewSender returns the raw sender, for a caller that builds its own pipeline or sends a
// batch itself.
func NewSender(opts ...Option) (*Sender, error) {
	s, _, err := newSender(opts...)
	return s, err
}

// newSender resolves the configuration once, so New and NewSender can never disagree.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		dsn:       os.Getenv("SENTRY_DSN"),
		allEvents: envBool("SENTRY_ALL_EVENTS"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	endpoint, publicKey, err := parseDSN(c.dsn)
	if err != nil {
		return nil, nil, err
	}
	client := httpdrain.New(endpoint,
		httpdrain.WithSource("sentry"),
		httpdrain.WithHeader("X-Sentry-Auth",
			"Sentry sentry_version=7, sentry_key="+publicKey+", sentry_client="+version.UserAgent()),
	)
	return &Sender{client: client, allEvents: c.allEvents}, c.pipelineOpts, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// parseDSN turns a Sentry DSN into the envelope endpoint and the public key. The DSN shape
// is <public_key>@<host>[:port][/<prefix>]/<project_id>, with or without a scheme: a DSN
// with no scheme means https. The prefix, when the DSN has one, stays before /api/.
func parseDSN(dsn string) (endpoint, publicKey string, err error) {
	if dsn == "" {
		return "", "", errors.New("sentry: SENTRY_DSN is required")
	}
	if !strings.Contains(dsn, "://") {
		dsn = "https://" + dsn
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
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	project := segments[len(segments)-1]
	if project == "" {
		return "", "", errors.New("sentry: DSN is missing the project id")
	}
	prefix := strings.Join(segments[:len(segments)-1], "/")
	if prefix != "" {
		prefix = "/" + prefix
	}
	if parsed.Host == "" {
		return "", "", errors.New("sentry: DSN is missing the host")
	}
	return fmt.Sprintf("%s://%s%s/api/%s/envelope/", parsed.Scheme, parsed.Host, prefix, project), publicKey, nil
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

// SendBatch sends one envelope per error event, because Sentry allows one event item per
// envelope, then one log envelope for the other events when AllEvents is on. A batch with
// nothing to send posts nothing and returns nil.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	var logs []map[string]any
	for _, event := range events {
		if isErrorEvent(event) {
			body, err := buildEventEnvelope(event)
			if err != nil {
				return err
			}
			if err := s.client.Post(ctx, body, contentType); err != nil {
				return err
			}
			continue
		}
		if s.allEvents {
			logs = append(logs, event)
		}
	}
	body, err := buildLogEnvelope(logs)
	if err != nil {
		return err
	}
	if body == nil {
		return nil
	}
	return s.client.Post(ctx, body, contentType)
}
