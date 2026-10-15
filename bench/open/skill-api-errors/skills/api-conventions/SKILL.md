---
name: api-conventions
description: Use when adding or changing HTTP endpoints in Notely. The team's rules for routes, responses and error codes; the API contract tests enforce them.
---

# Notely API conventions

- Register routes in `internal/api` with Go 1.22 patterns (`"GET /v1/notes/{id}"`).
- Success: `httpx.WriteJSON` with the resource itself as the body (not wrapped), status 200.
- Errors: always `httpx.WriteError(w, r, status, code, message)`; never `http.Error`.
- Error codes are snake_case and follow these rules:
  - a path or query parameter that cannot be parsed → status 400, code `invalid_<param>` (e.g. `invalid_id`)
  - a resource that does not exist → status 404, code `<resource>_not_found` (e.g. `note_not_found`)
- The message is one sentence for developers and must not echo the raw input.
