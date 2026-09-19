// Command rest-api is a small JSON REST API with wlog around every request. It is the
// rest-api recipe's example: chi routes, the default logger from the environment, and
// one wide event per request.
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremygprawira/wlog"
	wlogchi "github.com/jeremygprawira/wlog/middleware/chi"
)

// order is the shape the API answers.
type order struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Total  int    `json:"total_cents"`
}

// newRouter builds the API with one wide event per request.
func newRouter(logger *wlog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(wlogchi.Middleware(logger))

	r.Get("/orders/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		// The handler adds the field a searcher asks for, and wlog redacts it like
		// any other field.
		wlog.Set(req.Context(), "order_id", id)
		writeJSON(w, http.StatusOK, order{ID: id, Status: "paid", Total: 4821})
	})

	r.Post("/orders", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Total int `json:"total_cents"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			wlog.Error(req.Context(), err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad body"})
			return
		}
		wlog.Set(req.Context(), "order_total_cents", body.Total)
		writeJSON(w, http.StatusCreated, order{ID: "ord-1", Status: "created", Total: body.Total})
	})

	return r
}

// writeJSON writes one JSON answer.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func main() {
	logger := wlog.New(wlog.WithService("rest-api", "0.0.1", "local"))
	log.Fatal(http.ListenAndServe(":8080", newRouter(logger)))
}
