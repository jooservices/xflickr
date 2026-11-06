package rest

import (
	"net/http/httptest"
	"testing"
)

func TestIfMatch(t *testing.T) {
	r := httptest.NewRequest("PATCH", "/", nil)
	r.Header.Set("If-Match", ETag([]byte("v1")))
	if err := RequireIfMatch(r, ETag([]byte("v2"))); err == nil {
		t.Fatal("accepted stale write")
	}
}

func TestConditionalCases(t *testing.T) {
	tag := ETag([]byte("v1"))
	for _, tt := range []struct {
		header, current string
		ok              bool
	}{{tag, tag, true}, {`"other", ` + tag, tag, true}, {"*", tag, true}, {"*", "", false}, {"", tag, false}, {"W/" + tag, tag, false}, {"W/" + tag, "W/" + tag, false}} {
		r := httptest.NewRequest("PATCH", "/", nil)
		r.Header.Set("If-Match", tt.header)
		if err := RequireIfMatch(r, tt.current); (err == nil) != tt.ok {
			t.Fatal(tt, err)
		}
	}
}
