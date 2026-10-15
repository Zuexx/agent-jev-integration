// Package api is Notely's HTTP API.
package api

import (
	"net/http"

	"example.com/notely/internal/httpx"
	"example.com/notely/internal/store"
)

func NewServer(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/notes", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"notes": s.List()})
	})
	return mux
}
