package wlogstd

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
)

// statusWriter wraps http.ResponseWriter to observe the status code, byte count, and
// (when captureBody is set) up to maxCapture bytes of the response body, without
// changing what the real client receives.
type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool

	captureBody  bool
	allowedTypes []string
	maxCapture   int
	shouldCap    bool // decided at WriteHeader from the response's Content-Type
	buf          bytes.Buffer
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	if w.captureBody {
		w.shouldCap = typeAllowed(w.Header().Get("Content-Type"), w.allowedTypes)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.shouldCap && w.buf.Len() < w.maxCapture {
		remaining := w.maxCapture - w.buf.Len()
		if remaining > len(b) {
			remaining = len(b)
		}
		w.buf.Write(b[:remaining])
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// body returns the captured response body as a loggable value, or nil if nothing was
// captured (capture off, non-matching content type, or an empty body).
func (w *statusWriter) body() map[string]any {
	if !w.shouldCap {
		return nil
	}
	return parseBody(w.buf.Bytes())
}

// Flush implements http.Flusher when the underlying writer supports it (e.g. gzip or
// streaming middleware further up the chain).
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker when the underlying writer supports it, needed for
// WebSocket upgrades and similar to work through this middleware.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("wlogstd: underlying ResponseWriter does not support Hijack")
	}
	return h.Hijack()
}

// ReadFrom implements io.ReaderFrom when the underlying writer supports it, so
// io.Copy onto this writer keeps its fast path (e.g. http.ServeContent, sendfile).
// Response body capture still applies, same as Write, capped at maxCapture.
func (w *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	rf, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		n, err := io.Copy(struct{ io.Writer }{w}, r)
		return n, err
	}
	if w.shouldCap && w.buf.Len() < w.maxCapture {
		var buf bytes.Buffer
		tee := io.TeeReader(r, &buf)
		n, err := rf.ReadFrom(tee)
		captured := buf.Bytes()
		remaining := w.maxCapture - w.buf.Len()
		if remaining > len(captured) {
			remaining = len(captured)
		}
		w.buf.Write(captured[:remaining])
		w.bytes += int(n)
		return n, err
	}
	n, err := rf.ReadFrom(r)
	w.bytes += int(n)
	return n, err
}

// generateRequestID returns a random 16-byte id, hex-encoded, used when the request
// carries no X-Request-ID of its own.
func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(b)
}
