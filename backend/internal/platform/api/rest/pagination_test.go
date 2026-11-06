package rest

import (
	"net/url"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	encoded := EncodeCursor([]byte("timestamp:id"))
	got, err := DecodeCursor(encoded)
	if err != nil || string(got) != "timestamp:id" {
		t.Fatal(string(got), err)
	}
}

func TestLimitsAndInvalidCursors(t *testing.T) {
	for _, tt := range []struct {
		in    string
		want  int
		valid bool
	}{{"", 50, true}, {"1", 1, true}, {"100", 100, true}, {"0", 0, false}, {"101", 0, false}, {"bad", 0, false}, {"-1", 0, false}} {
		got, err := ParseLimit(tt.in)
		if got != tt.want || (err == nil) != tt.valid {
			t.Fatal(tt, got, err)
		}
	}
	if got, err := DecodeCursor(""); got != nil || err != nil {
		t.Fatal(got, err)
	}
	for _, value := range []string{"!", "=", EncodeCursor(make([]byte, 4097))} {
		if _, err := DecodeCursor(value); err == nil {
			t.Fatal(value)
		}
	}
}

func TestPageMeta(t *testing.T) {
	u, _ := url.Parse("/people?name=hello&cursor=old")
	meta := PageMeta(u, 50, "new")
	links := meta["links"].(map[string]string)
	next, err := url.Parse(links["next"])
	if err != nil || next.Query().Get("name") != "hello" || next.Query().Get("cursor") != "new" || u.Query().Get("cursor") != "old" {
		t.Fatal(meta, err)
	}
	if PageMeta(u, 50, "")["pagination"].(Pagination).HasMore {
		t.Fatal("has more")
	}
}
