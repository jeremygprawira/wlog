// Command scope_app is a fixture for the wlog map rule-scope tests: print logging, a swallowed
// error, an error branch, a denied key declared as a typed key, and the routes the sensitive
// matcher must read word by word.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// PasswordKey is a typed key whose name the redactor already denies.
var PasswordKey = wlog.NewKey[string]("password")

// fail is a helper that returns an error.
func fail() error { return errors.New("boom") }

func handlePrintWriter(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", "ord-1")
	fmt.Fprintf(w, "hello")
	w.WriteHeader(http.StatusOK)
}

func handlePrintStdout(w http.ResponseWriter, r *http.Request) {
	fmt.Println("hello")
	w.WriteHeader(http.StatusOK)
}

func handlePrintLog(w http.ResponseWriter, r *http.Request) {
	log.Println("hello")
	w.WriteHeader(http.StatusOK)
}

func handleSwallowedWriter(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleSwallowed(w http.ResponseWriter, r *http.Request) {
	_ = fail()
	w.WriteHeader(http.StatusOK)
}

func handleEmptyBranch(w http.ResponseWriter, r *http.Request) {
	if err := fail(); err != nil {
	}
	w.WriteHeader(http.StatusOK)
}

func handleReportedBranch(w http.ResponseWriter, r *http.Request) {
	if err := fail(); err != nil {
		wlog.Error(r.Context(), err)
	}
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()
	e := routes()
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(e))
}
