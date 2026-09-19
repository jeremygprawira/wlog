// This file holds body capture: the read that chains the rest of the body back to the
// handler, and the parse that turns the captured bytes into event fields. A body that was
// cut or that fails to parse becomes a marker, and its text is never kept.
package httpcore

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// Caps on a captured body. A larger cap is clamped, so one request cannot hold an
// unbounded buffer.
const (
	defaultMaxBody = 16 << 10 // 16 KiB per direction
	maxBodyLimit   = 1 << 20  // 1 MiB per direction
)

// defaultBodyTypes is the content type list of safe defaults. A type ending in /* matches
// a whole top-level type, and a type ending in /*+json matches a structured suffix.
var defaultBodyTypes = []string{
	"application/json", "application/*+json", "text/*", "application/x-www-form-urlencoded",
}

// CapturesBody reports whether the policy captures the body of this request, so an
// adapter reads it only when it must. A HEAD request has no body to read.
func (c *Core) CapturesBody(r Request) bool {
	if !c.cfg.captureBody || c.cfg.maxBody <= 0 {
		return false
	}
	if r.Method() == http.MethodHead {
		return false
	}
	return bodyTypeAllowed(c.cfg.bodyTypes, r.Header("Content-Type"))
}

// EchoesRequestID reports whether the adapter echoes the request id into the response,
// which an adapter that does not use NetHTTP reads before it writes the response.
func (c *Core) EchoesRequestID() bool { return c.cfg.echoRequestID }

// ReadBody reads at most MaxBody bytes of a body, and returns them with a reader that
// carries every byte, so the handler still sees the whole body.
//
// The returned bytes come from the pool of this Core, and the caller returns them with
// ReturnBody once the event holds its own copy. truncated reports that the body was
// longer than the cap.
func (c *Core) ReadBody(r io.Reader) (body []byte, rest io.Reader, truncated bool) {
	if r == nil || c.cfg.maxBody <= 0 {
		return nil, r, false
	}
	buf := c.pool.get()
	// One byte over the cap tells a body that exactly fits from one that does not.
	n, err := io.ReadFull(r, buf)
	if n == 0 && err != nil {
		c.pool.put(buf)
		return nil, r, false
	}
	rest = io.MultiReader(bytes.NewReader(buf[:n]), r)
	if n > c.cfg.maxBody {
		return buf[:c.cfg.maxBody], rest, true
	}
	return buf[:n], rest, false
}

// ReturnBody gives a buffer from ReadBody back to the pool.
func (c *Core) ReturnBody(body []byte) {
	if body != nil {
		c.pool.put(body)
	}
}

// RequestBody records the request body an adapter read. body holds at most MaxBody bytes,
// truncated reports that the body was longer, and decoded reports that the adapter
// already decoded a compressed body.
func (x *Exchange) RequestBody(body []byte, truncated, decoded bool) {
	if !x.core.cfg.captureBody || len(body) == 0 {
		return
	}
	if !decoded {
		if encoding := x.request.Header("Content-Encoding"); encoding != "" &&
			!strings.EqualFold(encoding, "identity") {
			return
		}
	}
	value := bodyValue(body, truncated, x.request.Header("Content-Type"))
	if value == nil {
		return
	}
	wlog.SetGroup(x.ctx, "http", "request_body", value)
}

// bodyValue turns captured bytes into an event value by the content type.
//
// A JSON body of any shape parses into the tree, so a key rule redacts inside it. A JSON
// body that was cut or that fails to parse, and a form body that was cut, become the
// truncated marker. A text body stays a string, which the reader already cut at the cap.
func bodyValue(body []byte, truncated bool, contentType string) any {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	switch {
	case isJSONType(mediaType):
		if truncated {
			return truncatedMarker(len(body))
		}
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			return truncatedMarker(len(body))
		}
		return value
	case mediaType == "application/x-www-form-urlencoded":
		if truncated {
			return truncatedMarker(len(body))
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return truncatedMarker(len(body))
		}
		return formValue(values)
	case strings.HasPrefix(mediaType, "text/"):
		return string(body)
	}
	return nil
}

// captureResponseBody records the response body when the policy and the response allow
// it. A HEAD request, a 204, and a 304 carry no body, and an encoded body is skipped.
func (c *Core) captureResponseBody(ctx context.Context, req Request, resp Response, status int, body []byte, truncated bool) {
	if !c.cfg.captureBody || len(body) == 0 || req == nil {
		return
	}
	if req.Method() == http.MethodHead || status == http.StatusNoContent || status == http.StatusNotModified {
		return
	}
	contentType, encoding := "", ""
	if resp != nil {
		resp.EachHeader(func(name, value string) {
			switch {
			case strings.EqualFold(name, "Content-Type"):
				contentType = value
			case strings.EqualFold(name, "Content-Encoding"):
				encoding = value
			}
		})
	}
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return
	}
	if value := bodyValue(body, truncated, contentType); value != nil {
		wlog.SetGroup(ctx, "http", "response_body", value)
	}
}

// truncatedMarker is the value a cut or unparsable body becomes. It holds no body text.
func truncatedMarker(bytes int) map[string]any {
	return map[string]any{"truncated": true, "bytes": bytes}
}

// formValue returns the first value of each form key, the shape the redactor walks.
func formValue(values url.Values) map[string]any {
	out := make(map[string]any, len(values))
	for key, list := range values {
		if len(list) > 0 {
			out[key] = list[0]
		}
	}
	return out
}

// isJSONType reports whether a media type is JSON, including a structured suffix such as
// application/vnd.api+json.
func isJSONType(mediaType string) bool {
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// bodyTypeAllowed reports whether a content type is one the policy captures.
func bodyTypeAllowed(types []string, contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	for _, allowed := range types {
		if mediaTypeMatches(allowed, mediaType) {
			return true
		}
	}
	return false
}

// mediaTypeMatches reports whether one media type matches an allowed pattern. The pattern
// matches exactly, or a trailing /* matches a whole top-level type, and a trailing /*+json
// matches a structured suffix.
func mediaTypeMatches(pattern, mediaType string) bool {
	pattern = strings.ToLower(pattern)
	switch {
	case pattern == mediaType:
		return true
	case strings.HasSuffix(pattern, "/*+json"):
		prefix := strings.TrimSuffix(pattern, "*+json")
		return strings.HasPrefix(mediaType, prefix) && strings.HasSuffix(mediaType, "+json")
	case strings.HasSuffix(pattern, "/*"):
		return strings.HasPrefix(mediaType, strings.TrimSuffix(pattern, "*"))
	}
	return false
}
