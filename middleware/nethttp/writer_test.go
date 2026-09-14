package wlogstd

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockHijacker adds http.Hijacker to httptest.NewRecorder, which doesn't implement it,
// so statusWriter's pass-through can be exercised.
type mockHijacker struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (m *mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	m.hijacked = true
	return nil, nil, nil
}

func TestStatusWriter_PreservesFlusherHijackerReaderFrom(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	var _ http.Flusher = sw
	var _ http.Hijacker = sw
	var _ io.ReaderFrom = sw

	sw.Flush() // rec implements Flusher; must not panic

	mh := &mockHijacker{ResponseRecorder: rec}
	sw2 := &statusWriter{ResponseWriter: mh, status: http.StatusOK}
	if _, _, err := sw2.Hijack(); err != nil {
		t.Fatalf("Hijack: %v", err)
	}
	if !mh.hijacked {
		t.Error("Hijack was not forwarded to the underlying ResponseWriter")
	}

	sw3 := &statusWriter{ResponseWriter: rec, status: http.StatusOK}
	if _, _, err := sw3.Hijack(); err == nil {
		t.Error("Hijack on a non-Hijacker ResponseWriter should error, not panic")
	}
}
