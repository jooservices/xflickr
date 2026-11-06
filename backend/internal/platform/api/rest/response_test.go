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

func TestDiscovery(t *testing.T) {
	data := Discovery(map[string]string{"people": "/people"})
	links := data["links"].(Links)
	if links["self"].Href != "/api/v1" || links["people"].Method != "GET" {
		t.Fatal(data)
	}
}
