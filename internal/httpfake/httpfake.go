// Package httpfake is a small httptest.Server wrapper that records every request it
// receives, for asserting on what a drain actually sent over the wire.
package httpfake

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

// Request is one recorded call.
type Request struct {
	Method  string
	Path    string
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
	header   http.Header
}

// New starts a recording server. Close it like any httptest.Server.
func New() *Server {
	s := &Server{status: http.StatusOK, header: http.Header{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Headers: r.Header.Clone(), Body: body})
	status := s.status
	for k, vs := range s.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	s.mu.Unlock()

	w.WriteHeader(status)
}

// SetStatus sets the status every subsequent request receives. Default 200.
func (s *Server) SetStatus(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
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
