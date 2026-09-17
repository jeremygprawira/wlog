// Command otherapp is a fixture in its own module, unrelated to cmd/wlog's. It proves that
// entry.LoadDir resolves a target by its own go.mod, not by the caller's module or workspace.
package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.ListenAndServe(":8080", mux)
}
