// Package httpx holds the HTTP helpers every handler uses.
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// WriteError writes the standard error envelope (see the api-conventions skill).
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteJSON(w, status, map[string]any{"error": map[string]string{
		"code": code, "message": message, "request_id": r.Header.Get("X-Request-ID"),
	}})
}
