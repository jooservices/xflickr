// Package rest provides the additive v1 HTTP contract. Legacy handlers opt in
// only when their consumers migrate.
package rest

import (
	"encoding/json"
	"net/http"
)

type Link struct {
	Href   string `json:"href"`
	Method string `json:"method,omitempty"`
}
type Links map[string]Link

func NewLink(href, method string) Link { return Link{Href: href, Method: method} }

type Envelope struct {
	Success bool           `json:"success"`
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    any            `json:"data"`
	Meta    map[string]any `json:"meta"`
	Errors  any            `json:"errors"`
	TraceID string         `json:"trace_id"`
}

func writeJSON(w http.ResponseWriter, status int, media string, body any) error {
	// Marshal before sending headers: an invalid representation must not produce
	// a partially written success response.
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", media)
	w.WriteHeader(status)
	_, err = w.Write(append(encoded, '\n'))
	return err
}

func WriteSuccess(w http.ResponseWriter, status int, message string, data any, meta map[string]any, traceID string) error {
	if meta == nil {
		meta = map[string]any{}
	}
	return writeJSON(w, status, "application/json", Envelope{true, status, message, data, meta, nil, traceID})
}

func Created(w http.ResponseWriter, location string, data any, traceID string) error {
	w.Header().Set("Location", location)
	return WriteSuccess(w, http.StatusCreated, "Created", data, nil, traceID)
}

func Accepted(w http.ResponseWriter, location string, data any, traceID string) error {
	w.Header().Set("Location", location)
	return WriteSuccess(w, http.StatusAccepted, "Accepted", data, nil, traceID)
}

func NoContent(w http.ResponseWriter) {
	w.Header().Del("Content-Type")
	w.WriteHeader(http.StatusNoContent)
}

type ProblemType struct {
	Slug   string
	Title  string
	Status int
}

// ProblemTypes returns a copy so a consumer cannot mutate the shared registry.
func ProblemTypes() []ProblemType {
	return []ProblemType{
		{"key-invalid", "Invalid API key", 422},
		{"temp-storage-full", "Temporary storage full", 503},
		{"reconnect-required", "Reconnect required", 409},
		{"account-locked", "Account locked", 429},
		{"token-expired", "Token expired", 409},
		{"validation-error", "Request validation failed", 422},
		{"not-found", "Not found", 404},
		{"conflict", "Conflict", 409},
		{"precondition-failed", "Precondition failed", 412},
		{"rate-limited", "Rate limited", 429},
		{"unauthenticated", "Authentication required", 401},
		{"forbidden", "Forbidden", 403},
		{"malformed-request", "Malformed request", 400},
		{"unsupported-media-type", "Unsupported media type", 415},
		{"internal-error", "Internal server error", 500},
	}
}

type (
	FieldErrors map[string][]string
	Problem     struct {
		Type     string      `json:"type"`
		Title    string      `json:"title"`
		Status   int         `json:"status"`
		Detail   string      `json:"detail"`
		Instance string      `json:"instance"`
		TraceID  string      `json:"trace_id"`
		Errors   FieldErrors `json:"errors"`
	}
)

func (p *Problem) Error() string { return p.Title }
func NewProblem(slug, detail string, fields FieldErrors) *Problem {
	for _, kind := range ProblemTypes() {
		if kind.Slug == slug {
			return &Problem{Type: "/problems/" + slug, Title: kind.Title, Status: kind.Status, Detail: detail, Errors: fields}
		}
	}
	return NewProblem("internal-error", "Internal server error", nil)
}

func WriteProblem(w http.ResponseWriter, r *http.Request, p *Problem, traceID string) error {
	body := *p
	body.Instance = r.URL.Path
	body.TraceID = traceID
	if body.Status == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
	}
	return writeJSON(w, body.Status, "application/problem+json", body)
}
