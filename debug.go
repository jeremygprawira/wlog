// This file holds the debug route of one Logger: one JSON object of Stats, for a
// /debug/wlog endpoint or for `wlog doctor --url`.
package wlog

import (
	"encoding/json"
	"net/http"
)

// DebugHandler serves the Logger's Stats as one JSON object per request. A debug route
// calls it, and a caller reads the numbers to find out why an event is missing.
func (l *Logger) DebugHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := json.Marshal(l.Stats())
		if err != nil {
			http.Error(w, "wlog: the stats did not encode", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}
