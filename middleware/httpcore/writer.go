// This file holds the response writer wrapper. It observes the status and the byte count,
// captures the body when the policy asks for it, and forwards every optional interface, so
// http.ResponseController and a streamed download still reach the real writer.
package httpcore

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// statusWriter observes one response and forwards every call to the writer the app gave.
//
// The wrapper never forwards a second WriteHeader, the same as net/http. A Flush before
// any write counts as status 200, and a successful Hijack records status 101.
type statusWriter struct {
	http.ResponseWriter
	status  int
	bytes   int64
	wrote   bool
	capture []byte // the captured body prefix, nil when the policy captures none
	max     int    // the most bytes the capture holds
}

// newStatusWriter wraps one response writer. capture is the body prefix to fill, or nil
// when the policy captures no body.
func newStatusWriter(w http.ResponseWriter, capture []byte, max int) *statusWriter {
	return &statusWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
		capture:        capture,
		max:            max,
	}
}

// Unwrap returns the writer this wrapper hides, so http.ResponseController reaches the
// real one for a deadline or a full duplex stream.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// WriteHeader records the first status the handler writes, and forwards it.
func (w *statusWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Write records the size of the body and the captured prefix, and forwards the bytes.
func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	w.appendBody(b)
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Flush forwards a flush, and counts a flush before any write as status 200.
func (w *statusWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards a hijack, and records status 101 when it succeeds.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, buffer, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	w.status = http.StatusSwitchingProtocols
	w.wrote = true
	return conn, buffer, nil
}

// ReadFrom copies a stream to the client, and captures at most the room left in the
// buffer. The rest goes straight to the real writer, so a large download is never held.
func (w *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	var total int64
	if w.capture != nil {
		if room := w.max - len(w.capture); room > 0 {
			n, err := io.Copy(onlyWriter{w}, io.LimitReader(r, int64(room)))
			total += n
			if err != nil {
				return total, err
			}
		}
	}
	readerFrom, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		n, err := io.Copy(onlyWriter{w.ResponseWriter}, r)
		w.bytes += n
		return total + n, err
	}
	n, err := readerFrom.ReadFrom(r)
	w.bytes += n
	return total + n, err
}

// appendBody adds as much of b as the capture buffer holds.
func (w *statusWriter) appendBody(b []byte) {
	if w.capture == nil {
		return
	}
	room := w.max - len(w.capture)
	if room <= 0 {
		return
	}
	if len(b) > room {
		b = b[:room]
	}
	w.capture = append(w.capture, b...)
}

// captured reports whether the capture buffer is full, so the caller knows the body was
// longer than the cap.
func (w *statusWriter) captured() ([]byte, bool) {
	if w.capture == nil {
		return nil, false
	}
	return w.capture, len(w.capture) == w.max
}

// onlyWriter hides the ReadFrom method of a writer, so an io.Copy reaches Write instead of
// calling ReadFrom again.
type onlyWriter struct {
	io.Writer
}
