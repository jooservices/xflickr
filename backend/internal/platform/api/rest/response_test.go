package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type brokenWriter struct{ http.ResponseWriter }

func (w brokenWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func TestResponseHelpers(t *testing.T) {
	for _, status := range []int{201, 202, 204} {
		w := httptest.NewRecorder()
		var err error
		switch status {
		case 201:
			err = Created(w, "/items/1", 1, "")
		case 202:
			err = Accepted(w, "/jobs/1", 1, "")
		case 204:
			NoContent(w)
		}
		if err != nil || w.Code != status {
			t.Fatal(w, err)
		}
		if status == 204 {
			if w.Body.Len() != 0 {
				t.Fatal(w.Body)
			}
		} else if w.Header().Get("Location") == "" {
			t.Fatal("missing location")
		}
	}
	w := httptest.NewRecorder()
	if err := WriteSuccess(w, 200, "", make(chan int), nil, ""); err == nil || w.Body.Len() != 0 {
		t.Fatal("marshal error")
	}
	if err := WriteSuccess(brokenWriter{httptest.NewRecorder()}, 200, "", nil, nil, ""); err == nil {
		t.Fatal("write error")
	}
}

func TestProblemRegistry(t *testing.T) {
	for _, kind := range ProblemTypes() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/test", nil)
		p := NewProblem(kind.Slug, "Safe detail", FieldErrors{"name": {"Invalid"}})
		if err := WriteProblem(w, r, p, "trace"); err != nil {
			t.Fatal(err)
		}
		var body Problem
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Type != "/problems/"+kind.Slug || body.Status != kind.Status || w.Code != kind.Status || body.Instance != r.URL.Path || body.TraceID != "trace" || body.Errors["name"][0] != "Invalid" || p.Error() != kind.Title {
			t.Fatal(body)
		}
		if kind.Status == 401 && w.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("missing challenge")
		}
		if w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatal(w.Header())
		}
	}
	if NewProblem("not-registered", "unsafe", nil).Status != 500 {
		t.Fatal("unknown type")
	}
}

func TestWriteProblemAt(t *testing.T) {
	w := httptest.NewRecorder()
	problem := NewProblem("email-taken", "Email is already registered", FieldErrors{"email": {"Already registered"}})
	if err := WriteProblemAt(w, "/api/v1/users", problem, "trace-id"); err != nil {
		t.Fatal(err)
	}
	var body Problem
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || w.Header().Get("Content-Type") != "application/problem+json" || body.Type != "/problems/email-taken" || body.Instance != "/api/v1/users" || body.TraceID != "trace-id" || body.Errors["email"][0] == "" {
		t.Fatalf("response = %d %q %+v", w.Code, w.Header().Get("Content-Type"), body)
	}
}

func TestDiscovery(t *testing.T) {
	data := Discovery(map[string]Link{"people": NewLink("/api/v1/people", "GET"), "users": NewLink("/api/v1/users", "POST")})
	links := data["links"].(Links)
	if links["self"].Href != "/api/v1" || links["people"].Method != "GET" || links["users"].Method != "POST" {
		t.Fatal(data)
	}
}
