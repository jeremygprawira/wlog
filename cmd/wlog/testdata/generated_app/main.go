// Command generated_app is a fixture for the generated-file test: its only handler sits in a
// generated file, which the map must not score.
package main

import "net/http"

func main() {
	http.ListenAndServe(":8080", Routes())
}
