// This file holds the capture policy: which request and response fields safe defaults
// keep, and what CaptureAll adds. A value the policy does not capture never reaches the
// event, so a session token cannot leak through a field nobody asked for.
package httpcore

import (
	"context"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// maskedValue is the text a cookie value carries when CookieValues does not list it.
const maskedValue = "[REDACTED]"

// maxNames caps the query keys and the cookie names one event carries, so a crafted
// query string cannot fill the event.
const maxNames = 50

// defaultRequestHeaders is the request header allow-list of safe defaults.
var defaultRequestHeaders = []string{
	"accept", "accept-language", "content-type", "content-length", "origin",
	"referer", "idempotency-key", "x-forwarded-proto", "x-forwarded-host",
}

// defaultResponseHeaders is the response header allow-list of safe defaults.
var defaultResponseHeaders = []string{
	"content-type", "content-length", "cache-control", "location", "retry-after",
}

// captureRequest writes the request fields the policy allows.
func (c *Core) captureRequest(ctx context.Context, r Request) {
	if headers := c.requestHeaders(r); len(headers) > 0 {
		wlog.SetGroup(ctx, "http", "request_headers", headers)
	}
	keys, values := c.query(r)
	if len(keys) > 0 {
		wlog.SetGroup(ctx, "http", "request_query_keys", keys)
	}
	if len(values) > 0 {
		wlog.SetGroup(ctx, "http", "request_query", values)
	}
	names, cookies := c.cookies(r)
	if len(names) > 0 {
		wlog.SetGroup(ctx, "http", "request_cookie_names", names)
	}
	if len(cookies) > 0 {
		wlog.SetGroup(ctx, "http", "request_cookies", cookies)
	}
	if c.cfg.user != nil {
		if id := c.cfg.user(r); id != "" {
			wlog.SetGroup(ctx, "user", "id", id)
		}
	}
}

// requestHeaders returns the request headers the policy keeps, by lowercase name. Safe
// defaults keep the allow-list, and CaptureAll keeps every header.
func (c *Core) requestHeaders(r Request) map[string]any {
	out := map[string]any{}
	r.EachHeader(func(name, value string) {
		lower := strings.ToLower(name)
		if !c.cfg.captureAll && !c.cfg.requestHeaders[lower] {
			return
		}
		if _, taken := out[lower]; !taken {
			out[lower] = value
		}
	})
	return out
}

// query returns the sorted query key names, and the values when the policy captures
// them.
func (c *Core) query(r Request) ([]any, map[string]any) {
	first := map[string]string{}
	r.EachQuery(func(key, value string) {
		if _, taken := first[key]; !taken {
			first[key] = value
		}
	})
	keys := sortedNames(first)
	if !c.cfg.captureAll {
		return keys, nil
	}
	values := make(map[string]any, len(first))
	for key, value := range first {
		values[key] = value
	}
	return keys, values
}

// cookies returns the sorted cookie names, and the values when the policy captures them.
// A value is masked unless CookieValues lists its name.
func (c *Core) cookies(r Request) ([]any, map[string]any) {
	first := map[string]string{}
	r.EachCookie(func(name, value string) {
		first[name] = value
	})
	names := sortedNames(first)
	if !c.cfg.captureAll {
		return names, nil
	}
	values := make(map[string]any, len(first))
	for name, value := range first {
		if c.cfg.cookieValues[name] {
			values[name] = value
			continue
		}
		values[name] = maskedValue
	}
	return names, values
}

// responseHeaders returns the response headers the policy keeps, by lowercase name. The
// per-route rules of the matching route replace the base policy.
func (c *Core) responseHeaders(ctx context.Context, cfg config, resp Response) {
	if resp == nil {
		return
	}
	out := map[string]any{}
	resp.EachHeader(func(name, value string) {
		lower := strings.ToLower(name)
		if !cfg.captureAll && !cfg.responseHeaders[lower] {
			return
		}
		if _, taken := out[lower]; !taken {
			out[lower] = value
		}
	})
	if len(out) > 0 {
		wlog.SetGroup(ctx, "http", "response_headers", out)
	}
}

// sortedNames returns the keys of a map, sorted, and cut to the name cap.
func sortedNames(values map[string]string) []any {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxNames {
		names = names[:maxNames]
	}
	out := make([]any, len(names))
	for i, name := range names {
		out[i] = name
	}
	return out
}

// headerSet returns a set of lowercase header names.
func headerSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[strings.ToLower(name)] = true
	}
	return out
}
