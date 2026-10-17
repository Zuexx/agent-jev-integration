// Package api is Notely's HTTP API.
package api

import (
	"net/http"
	"strconv"

	"example.com/notely/internal/httpx"
	"example.com/notely/internal/store"
)

func NewServer(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/notes", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"notes": s.List()})
	})
	mux.HandleFunc("GET /v1/notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_id", "The note id must be an integer.")
			return
		}
		n, ok := s.Get(id)
		if !ok {
			httpx.WriteError(w, r, http.StatusNotFound, "note_not_found", "No note has this id.")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, n)
	})
	return mux
}
