package main

import "net/http"

// routes registers the handlers of the scope fixture, so the map has something to score.
func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", handlePrintWriter)
	mux.HandleFunc("GET /print-stdout", handlePrintStdout)
	mux.HandleFunc("GET /print-log", handlePrintLog)
	mux.HandleFunc("GET /swallowed-writer", handleSwallowedWriter)
	mux.HandleFunc("GET /swallowed", handleSwallowed)
	mux.HandleFunc("GET /empty-branch", handleEmptyBranch)
	mux.HandleFunc("GET /reported-branch", handleReportedBranch)
	return mux
}
