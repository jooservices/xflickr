package rest

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSuccessContract(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteSuccess(w, 200, "Success", map[string]any{"links": Links{"self": NewLink("/api/v1", "GET")}}, nil, "trace"); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 7 || body["code"] != float64(200) || body["success"] != true || body["errors"] != nil || body["trace_id"] != "trace" {
		t.Fatal(body)
	}
}

// This fixture is intentionally data, so JOOXF-317 can apply the same cases to
// real route responses without importing a test-only Go implementation.
func TestConformanceCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name        string `json:"name"`
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Body        bool   `json:"body"`
		Location    bool   `json:"location"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v1", nil)
			var err error
			switch tt.Status {
			case 200:
				err = WriteSuccess(w, 200, "Success", Discovery(nil), nil, "trace")
			case 201:
				err = Created(w, "/items/1", Discovery(nil), "trace")
			case 202:
				err = Accepted(w, "/jobs/1", Discovery(nil), "trace")
			case 204:
				NoContent(w)
			default:
				w.Header().Set("Retry-After", "60")
				err = WriteProblem(w, r, NewProblem(tt.Name, "Safe detail", nil), "trace")
			}
			if err != nil || w.Code != tt.Status || w.Header().Get("Content-Type") != tt.ContentType || (w.Body.Len() > 0) != tt.Body || (w.Header().Get("Location") != "") != tt.Location {
				t.Fatal(tt, w, err)
			}
		})
	}
}
