// This file holds the options of one Core, and the trusted-proxy rules that read a
// forwarded header only from a proxy the app named.
package httpcore

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Option configures one Core.
type Option func(*config)

// config holds the resolved options of one Core.
type config struct {
	route          func(*http.Request) (string, bool)
	trustedProxies []netip.Prefix
}

// newConfig resolves the options of one Core.
func newConfig(opts []Option) config {
	cfg := config{route: requestPattern}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
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

// hostOf returns the address part of an ip:port string, and the whole string when it
// carries no port.
func hostOf(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
