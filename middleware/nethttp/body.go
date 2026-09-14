package wlogstd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const defaultMaxBodyCapture = 10 * 1024

// defaultBodyContentTypes are the types captured by default: JSON and any text/* type.
// A binary type (images, octet-stream, ...) is skipped so it is never buffered or
// corrupted by capture.
var defaultBodyContentTypes = []string{"application/json", "text/"}

// typeAllowed reports whether contentType (a raw Content-Type header value, params
// and all) matches one of allowed. An entry ending in "/" matches as a prefix
// (a whole top-level type, e.g. "text/"); otherwise it must match exactly (ignoring
// any ";charset=..." suffix).
func typeAllowed(contentType string, allowed []string) bool {
	if contentType == "" {
		return false
	}
	ct := contentType
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(ct)
	for _, a := range allowed {
		if strings.HasSuffix(a, "/") {
			if strings.HasPrefix(ct, a) {
				return true
			}
		} else if ct == a {
			return true
		}
	}
	return false
}

// parseBody turns captured bytes into a loggable value: parsed JSON if it is JSON,
// else the raw text under "raw".
func parseBody(data []byte) map[string]any {
	if len(data) == 0 {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err == nil {
		return parsed
	}
	return map[string]any{"raw": string(data)}
}

// captureRequestBody reads up to maxBytes of r.Body for logging, then restores r.Body
// so the handler still sees the complete, untruncated stream: the captured prefix
// followed by whatever remains unread on the original body. It never buffers more
// than maxBytes (gate G4).
func captureRequestBody(r *http.Request, maxBytes int, allowed []string) map[string]any {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	if !typeAllowed(r.Header.Get("Content-Type"), allowed) {
		return nil
	}

	buf := make([]byte, maxBytes)
	n, _ := io.ReadFull(r.Body, buf) // n < maxBytes with an EOF/UnexpectedEOF is fine
	captured := buf[:n]

	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(captured), r.Body), r.Body}

	return parseBody(captured)
}
