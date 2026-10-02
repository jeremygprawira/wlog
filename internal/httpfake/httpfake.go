// Package httpfake is a small httptest.Server wrapper that records every request it
// receives, for asserting on what a drain actually sent over the wire.
package httpfake

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
)

// Request is one recorded call.
type Request struct {
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    []byte
}

// Server records every request and replies with a configurable status (200 by
// default, changeable via SetStatus) and header (for Retry-After, say).
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	requests []Request
	status   int
	body     []byte
	header   http.Header
}

// New starts a recording server. Close it like any httptest.Server.
func New() *Server {
	s := &Server{status: http.StatusOK, header: http.Header{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body := Body(r)

	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Headers: r.Header.Clone(), Body: body})
	status := s.status
	reply := s.body
	for k, vs := range s.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	s.mu.Unlock()

	w.WriteHeader(status)
	_, _ = w.Write(reply)
}

// Body reads a request body, and decodes it when the request says it is gzip, so a fake
// sees what a real backend sees.
func Body(r *http.Request) []byte {
	body, _ := io.ReadAll(r.Body)
	if r.Header.Get("Content-Encoding") != "gzip" {
		return body
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return body
	}
	defer func() { _ = zr.Close() }()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		return body
	}
	return decoded
}

// SetStatus sets the status every subsequent request receives. Default 200.
func (s *Server) SetStatus(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// SetBody sets the response body every subsequent request receives. Default empty.
func (s *Server) SetBody(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = []byte(body)
}

// SetHeader sets a response header every subsequent request receives (e.g. Retry-After).
func (s *Server) SetHeader(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.header.Set(key, value)
}

// Requests returns every request recorded so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.requests))
	copy(out, s.requests)
	return out
}

// Last returns the most recent request, or nil if none has arrived yet.
func (s *Server) Last() *Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		return nil
	}
	return &s.requests[len(s.requests)-1]
}
