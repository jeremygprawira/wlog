// Command swallowed is a fixture for the wlog map rule tests. Two handlers discard an
// error, one reports it, and one returns it to a caller the rule cannot see.
package main

import (
	"errors"
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func doWork() error { return errors.New("boom") }

func handleDiscard(w http.ResponseWriter, r *http.Request) {
	_ = doWork()
	w.WriteHeader(http.StatusOK)
}

func handleEmptyBranch(w http.ResponseWriter, r *http.Request) {
	err := doWork()
	if err != nil {
	}
	w.WriteHeader(http.StatusOK)
}

func handleGood(w http.ResponseWriter, r *http.Request) {
	if err := doWork(); err != nil {
		wlog.Error(r.Context(), err)
	}
	w.WriteHeader(http.StatusOK)
}

// handleUnresolvable hands the error to a goroutine the rule cannot see, so the rule
// stays quiet.
func handleUnresolvable(w http.ResponseWriter, r *http.Request) {
	if err := doWork(); err != nil {
		go reportAsync(err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func reportAsync(err error) { _ = err }

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/discard", handleDiscard)
	mux.HandleFunc("/empty", handleEmptyBranch)
	mux.HandleFunc("/good", handleGood)
	mux.HandleFunc("/unresolvable", handleUnresolvable)
	http.ListenAndServe(":8080", wlogstd.Middleware(wlog.New())(mux))
}
