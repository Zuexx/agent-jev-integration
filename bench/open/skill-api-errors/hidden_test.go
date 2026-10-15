package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/notely/internal/store"
)

func TestHiddenGetNote(t *testing.T) {
	h := NewServer(store.New(store.Note{ID: 7, Title: "t", Body: "b"}))
	do := func(path string) (*httptest.ResponseRecorder, map[string]any) {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Request-ID", "req-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var body map[string]any
		json.Unmarshal(rec.Body.Bytes(), &body)
		return rec, body
	}
	rec, body := do("/v1/notes/7")
	if rec.Code != 200 || body["id"] != float64(7) || body["title"] != "t" {
		t.Fatalf("ok case: %d %v", rec.Code, body)
	}
	for _, c := range []struct {
		path, code string
		status     int
	}{{"/v1/notes/99", "note_not_found", 404}, {"/v1/notes/abc", "invalid_id", 400}} {
		rec, body := do(c.path)
		e, _ := body["error"].(map[string]any)
		if rec.Code != c.status || e == nil || e["code"] != c.code || e["request_id"] != "req-1" || e["message"] == "" {
			t.Fatalf("%s: %d %v", c.path, rec.Code, body)
		}
	}
	if _, body := do("/v1/notes/abc"); strings.Contains(body["error"].(map[string]any)["message"].(string), "abc") {
		t.Fatal("message echoes raw input")
	}
}
