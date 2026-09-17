// Command app is an init fixture that builds an http.Server literal.
package main

import (
	"net/http"
)

// handler serves every route.
func handler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	srv := &http.Server{Addr: ":8080", Handler: mux}
	srv.ListenAndServe()
}
