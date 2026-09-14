package wlogstd

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// statusWriter wraps http.ResponseWriter to observe the status code and byte count
// without changing what the real client receives.
type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Flush implements http.Flusher when the underlying writer supports it (e.g. gzip or
// streaming middleware further up the chain).
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
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
