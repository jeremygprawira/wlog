// Command app is an init fixture with a net/http router.
package main

import (
	"net/http"
	"os"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// ADDR lets a test bind a free port; the default is the usual one.
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	http.ListenAndServe(addr, mux)
}
