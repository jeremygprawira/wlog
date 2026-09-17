// Package handler is the other package in the crosspkg fixture: the map must find these
// handlers even though the routes are registered somewhere else.
package handler

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
)

// Orders is a named handler in another package.
func Orders(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", r.PathValue("id"))
	w.WriteHeader(http.StatusOK)
}

// Service is a handler factory: Refund returns the handler that will run.
type Service struct{}

// Refund returns a closure, so the map must report the factory.
func (s Service) Refund() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "refund_id", r.PathValue("id"))
		w.WriteHeader(http.StatusAccepted)
	}
}

// AuditHandler is a type that serves requests itself.
type AuditHandler struct{}

// ServeHTTP makes AuditHandler an http.Handler.
func (h AuditHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "audit", true)
	w.WriteHeader(http.StatusOK)
}
