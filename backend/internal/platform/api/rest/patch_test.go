package rest

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPatchAllowlist(t *testing.T) {
	r := httptest.NewRequest("PATCH", "/", strings.NewReader(`{"role":"admin"}`))
	r.Header.Set("Content-Type", "application/merge-patch+json")
	_, err := ReadMergePatch(r, 1024, "name")
	if err == nil {
		t.Fatal("accepted unknown field")
	}
}

func TestMergePatchCases(t *testing.T) {
	for _, tt := range []struct {
		body, media string
		limit       int64
		status      int
	}{{`{"name":null}`, "application/merge-patch+json", 1024, 0}, {`{}`, "application/merge-patch+json; charset=utf-8", 1024, 0}, {`null`, "application/merge-patch+json", 1024, 400}, {`[]`, "application/merge-patch+json", 1024, 400}, {`{"name":1} {}`, "application/merge-patch+json", 1024, 400}, {`{`, "application/merge-patch+json", 1024, 400}, {`{"name":"long"}`, "application/merge-patch+json", 3, 400}, {`{}`, "application/merge-patch+json", 0, 400}, {`{}`, "application/json", 1024, 415}} {
		r := httptest.NewRequest("PATCH", "/", strings.NewReader(tt.body))
		r.Header.Set("Content-Type", tt.media)
		got, err := ReadMergePatch(r, tt.limit, "name")
		var problem *Problem
		if tt.status == 0 {
			if err != nil {
				t.Fatal(tt, err)
			}
			if tt.body == `{"name":null}` && string(got["name"]) != "null" {
				t.Fatal(got)
			}
		} else if !errors.As(err, &problem) || problem.Status != tt.status {
			t.Fatal(tt, err)
		}
	}
}
