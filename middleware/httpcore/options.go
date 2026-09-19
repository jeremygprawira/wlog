// This file holds the options of one Core, the path and route globs, and the
// trusted-proxy rules that read a forwarded header only from a proxy the app named.
package httpcore

import (
	"maps"
	"net"
	"net/http"
	"net/netip"
	pathpkg "path"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// Option configures one Core.
type Option func(*config)

// config holds the resolved options of one Core.
type config struct {
	route          func(*http.Request) (string, bool)
	trustedProxies []netip.Prefix

	captureAll      bool
	captureBody     bool
	bodySet         bool // true once CaptureBody named the answer itself
	maxBody         int
	bodyTypes       []string
	requestHeaders  map[string]bool // lowercase allow-list
	responseHeaders map[string]bool // lowercase allow-list
	cookieValues    map[string]bool // cookie names kept unmasked
	skipPaths       []string
	skip            func(Request) bool
	routes          []routeRule
	trustRequestID  bool
	echoRequestID   bool
	user            func(Request) string
}

// routeRule is one ForRoute rule: the "METHOD template" pattern, and the options it
// applies to a request whose route matches.
type routeRule struct {
	pattern string
	opts    []Option
}

// newConfig resolves the options of one Core. A development service environment captures
// everything by default, and an explicit option still wins.
func newConfig(log *wlog.Logger, opts []Option) config {
	cfg := config{
		route:           requestPattern,
		maxBody:         defaultMaxBody,
		bodyTypes:       defaultBodyTypes,
		requestHeaders:  headerSet(defaultRequestHeaders),
		responseHeaders: headerSet(defaultResponseHeaders),
		cookieValues:    map[string]bool{},
		trustRequestID:  true,
		echoRequestID:   true,
	}
	if log != nil && isLocalEnv(log.ServiceEnv()) {
		cfg.captureAll = true
		cfg.captureBody = true
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// isLocalEnv reports whether a service environment is a development one.
func isLocalEnv(env string) bool {
	switch env {
	case "local", "dev", "development":
		return true
	}
	return false
}

// RouteFunc sets how the adapter reads the route template of a finished request. The
// default reads the pattern that net/http's own ServeMux matched.
func RouteFunc(fn func(*http.Request) string) Option {
	return func(c *config) {
		if fn == nil {
			return
		}
		c.route = func(r *http.Request) (string, bool) {
			template := fn(r)
			return template, template != ""
		}
	}
}

// CaptureAll captures every header, the query values, the cookie values, the path
// parameter values, and the bodies. A development service environment turns it on. An
// explicit CaptureBody wins over this option.
func CaptureAll() Option {
	return func(c *config) {
		c.captureAll = true
		if !c.bodySet {
			c.captureBody = true
		}
	}
}

// CaptureBody captures the request and the response body without the rest of CaptureAll.
// A body is captured only for the content types of BodyTypes, and never for a HEAD
// request or for a response with no body.
func CaptureBody(on bool) Option {
	return func(c *config) {
		c.captureBody = on
		c.bodySet = true
	}
}

// MaxBody caps the bytes captured in each direction. The handler still reads the whole
// request body. The default is 16 KiB, and the value is clamped to 0 through 1 MiB.
func MaxBody(bytes int) Option {
	return func(c *config) {
		if bytes < 0 {
			bytes = 0
		}
		if bytes > maxBodyLimit {
			bytes = maxBodyLimit
		}
		c.maxBody = bytes
	}
}

// BodyTypes sets the content types whose bodies are captured. A type ending in /* matches
// a whole top-level type, and one ending in /*+json matches a structured suffix.
func BodyTypes(types ...string) Option {
	return func(c *config) {
		if len(types) > 0 {
			c.bodyTypes = types
		}
	}
}

// CaptureHeaders adds names to the request header allow-list of safe defaults.
func CaptureHeaders(names ...string) Option {
	return func(c *config) {
		for _, name := range names {
			c.requestHeaders[strings.ToLower(name)] = true
		}
	}
}

// CaptureResponseHeaders adds names to the response header allow-list of safe defaults.
func CaptureResponseHeaders(names ...string) Option {
	return func(c *config) {
		for _, name := range names {
			c.responseHeaders[strings.ToLower(name)] = true
		}
	}
}

// CookieValues names the cookies whose values stay unmasked under CaptureAll. Every other
// cookie value is masked, so a session token cannot leak.
func CookieValues(names ...string) Option {
	return func(c *config) {
		for _, name := range names {
			c.cookieValues[name] = true
		}
	}
}

// SkipPaths skips a path entirely, so no event starts for it. A pattern is exact, or a
// glob where * matches one path segment and ** matches zero or more segments.
func SkipPaths(patterns ...string) Option {
	return func(c *config) { c.skipPaths = append(c.skipPaths, patterns...) }
}

// Skip skips a request the function rejects, so no event starts for it.
func Skip(fn func(Request) bool) Option {
	return func(c *config) { c.skip = fn }
}

// ForRoute adds a rule that applies to a matched route. The pattern is "METHOD template",
// and it may be a glob. The rule changes the response fields of a matching request.
func ForRoute(pattern string, opts ...Option) Option {
	return func(c *config) { c.routes = append(c.routes, routeRule{pattern: pattern, opts: opts}) }
}

// TrustRequestID keeps an incoming X-Request-ID header at 128 characters or fewer from
// the allowed alphabet. Default true.
func TrustRequestID(on bool) Option { return func(c *config) { c.trustRequestID = on } }

// EchoRequestID writes the request id on the X-Request-ID response header. Default true.
func EchoRequestID(on bool) Option { return func(c *config) { c.echoRequestID = on } }

// User sets user.id from the request, such as the authenticated subject.
func User(fn func(Request) string) Option { return func(c *config) { c.user = fn } }

// TrustedProxies names the proxy addresses whose forwarded headers count. An address may
// be one address or a CIDR. With none set, every forwarded header from a client is
// ignored, because a client can write any header it likes.
func TrustedProxies(cidrs ...string) Option {
	return func(c *config) {
		for _, cidr := range cidrs {
			prefix, err := parsePrefix(cidr)
			if err != nil {
				// A name that is not an address trusts nothing, which is the safe
				// reading of a typo.
				continue
			}
			c.trustedProxies = append(c.trustedProxies, prefix)
		}
	}
}

// parsePrefix reads one trusted address, which may be a single address or a CIDR.
func parsePrefix(cidr string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(cidr); err == nil {
		return prefix, nil
	}
	addr, err := netip.ParseAddr(cidr)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// trusted reports whether an address is one of the configured proxies.
func (c *Core) trusted(addr netip.Addr) bool {
	for _, prefix := range c.cfg.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// trustedRemote reports whether the connection came from a configured proxy.
func (c *Core) trustedRemote(r Request) bool {
	addr, err := netip.ParseAddr(hostOf(r.RemoteAddr()))
	return err == nil && c.trusted(addr)
}

// clientIP returns the client address of one request.
//
// With no trusted proxy it is the connection address. From a trusted proxy it is the
// first address of X-Forwarded-For that is not itself a trusted proxy, walking from the
// right, so a client cannot prepend a claim of its own.
func (c *Core) clientIP(r Request) string {
	host := hostOf(r.RemoteAddr())
	if !c.trustedRemote(r) {
		return host
	}
	forwarded := r.Header("X-Forwarded-For")
	if forwarded == "" {
		return host
	}
	parts := strings.Split(forwarded, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		addr, err := netip.ParseAddr(candidate)
		if err != nil {
			continue
		}
		if !c.trusted(addr) {
			return candidate
		}
	}
	return host
}

// schemeHost returns the scheme and the host of one request.
//
// A TLS connection is https, and the Host header names the host. From a trusted proxy,
// X-Forwarded-Proto and X-Forwarded-Host replace both.
func (c *Core) schemeHost(r Request) (string, string) {
	scheme := "http"
	if reader, ok := r.(SchemeReader); ok {
		scheme = reader.Scheme()
	}
	host := r.Header("Host")
	if !c.trustedRemote(r) {
		return scheme, host
	}
	if forwarded := r.Header("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	if forwarded := r.Header("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}
	return scheme, host
}

// routeConfig returns the policy of one finished request. A ForRoute rule whose pattern
// matches "METHOD template" replaces the base policy, on its own copy of the allow-lists.
func (c *Core) routeConfig(method, route string) config {
	if route == "" || len(c.cfg.routes) == 0 {
		return c.cfg
	}
	target := method + " " + route
	for _, rule := range c.cfg.routes {
		if !globMatch(rule.pattern, target) {
			continue
		}
		cfg := c.cfg
		cfg.requestHeaders = maps.Clone(cfg.requestHeaders)
		cfg.responseHeaders = maps.Clone(cfg.responseHeaders)
		cfg.cookieValues = maps.Clone(cfg.cookieValues)
		for _, opt := range rule.opts {
			opt(&cfg)
		}
		return cfg
	}
	return c.cfg
}

// globMatch reports whether a value matches a pattern. A pattern with no wildcard matches
// one value. A * matches one segment and a ** matches zero or more segments, so a **
// crosses a slash.
func globMatch(pattern, target string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(target, "/"))
}

// matchSegments matches the segments of a pattern against the segments of a value.
func matchSegments(pattern, target []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			if matchSegments(pattern[1:], target) {
				return true
			}
			if len(target) == 0 {
				return false
			}
			target = target[1:]
			continue
		}
		if len(target) == 0 {
			return false
		}
		if ok, _ := pathpkg.Match(pattern[0], target[0]); !ok {
			return false
		}
		pattern, target = pattern[1:], target[1:]
	}
	return len(target) == 0
}

// hostOf returns the address part of an ip:port string, and the whole string when it
// carries no port.
func hostOf(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
