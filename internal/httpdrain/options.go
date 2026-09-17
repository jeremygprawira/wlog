package httpdrain

import (
	"net/http"
	"time"
)

// Option configures a Client built by New.
type Option func(*Client)

// defaultTimeout bounds one drain request when the caller gives no other client or
// timeout, so a black-holed backend cannot hold a retrying pipeline forever.
const defaultTimeout = 10 * time.Second

// WithHTTPClient uses a caller-supplied HTTP client, for a custom transport, a proxy,
// or instrumentation. It clears any timeout set by an earlier WithTimeout, because the
// caller's client already carries its own. A later WithTimeout still applies, so the
// last option wins. wlog never changes the client it is given.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client == nil {
			return
		}
		c.httpClient = client
		c.timeoutSet = false
	}
}

// WithTimeout sets the HTTP request timeout. Default 10s. The timeout applies to a
// copy of the client, so a client the caller shared with the rest of the app keeps
// its own settings.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.timeout = d
		c.timeoutSet = true
	}
}

// WithHeader sets one extra header on every request (e.g. an API key).
func WithHeader(key, value string) Option {
	return func(c *Client) { c.headers[key] = value }
}

// WithHeaderFunc sets a function that computes extra headers from the request body,
// for a header that depends on the body, such as an HMAC signature. The body is the
// uncompressed bytes, so a signature stays valid when gzip is also on. A computed
// header overwrites a fixed one with the same name.
func WithHeaderFunc(fn func(body []byte) map[string]string) Option {
	return func(c *Client) { c.headerFunc = fn }
}

// WithGzip compresses the body and sets Content-Encoding: gzip when on.
func WithGzip(on bool) Option { return func(c *Client) { c.gzip = on } }

// WithSource sets X-Wlog-Source (e.g. "axiom", "loki"). Empty (the default) omits
// the header.
func WithSource(name string) Option { return func(c *Client) { c.source = name } }

// WithUserAgent overrides the User-Agent header; an empty string omits it entirely.
// Default "wlog/<version>".
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }
