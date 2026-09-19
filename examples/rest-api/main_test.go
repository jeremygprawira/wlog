// This file serves one request and compares the event with the recipe's hand-written
// golden. The schema tool validates the golden against schema/event.v1.json.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog/internal/conformance"
)

// TestRestAPI_GoldenEvent proves that one GET /orders/{id} gives the event the recipe
// documents.
func TestRestAPI_GoldenEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	router := newRouter(rec.Logger())

	request := httptest.NewRequest(http.MethodGet, "/orders/42", nil)
	request.Header.Set("X-Request-ID", "recipe-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	events := rec.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	want := golden(t)
	got := conformance.Normalize(events[0])
	if diff := conformance.Diff(conformance.Normalize(want), got); diff != "" {
		t.Errorf("the event differs from the golden:\n%s", diff)
	}
}

// golden reads the recipe's hand-written event.
func golden(t *testing.T) map[string]any {
	t.Helper()
	body, err := os.ReadFile("testdata/event.json")
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	event := map[string]any{}
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatalf("parse the golden: %v", err)
	}
	return event
}
