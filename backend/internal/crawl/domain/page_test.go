package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseCursor(t *testing.T) {
	cursor, err := ParseCursor([]byte(`{"method":"flickr.contacts.getList"}`))
	if err != nil || cursor.Page != 1 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	cursor, err = ParseCursor([]byte(`{"page":4,"method":"m","params":{"a":"b"}}`))
	if err != nil || cursor.Page != 4 || cursor.Params["a"] != "b" || cursor.Next().Page != 5 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	for _, bad := range []string{``, `{}`, `{"page":-1,"method":"m"}`, `[1]`} {
		if _, err := ParseCursor([]byte(bad)); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("%q: err = %v", bad, err)
		}
	}
}

func TestNewCursorStartsAtFirstPage(t *testing.T) {
	cursor := NewCursor(WorkSpec{Subject: "s", Method: "m", Params: map[string]string{"a": "b"}})
	if cursor.Page != 1 || cursor.Method != "m" || cursor.Params["a"] != "b" {
		t.Fatalf("cursor = %+v", cursor)
	}
}

func TestNewCursorMovesContinuationIntoCursor(t *testing.T) {
	params := map[string]string{"user_id": "owner", "continuation": "0"}
	cursor := NewCursor(WorkSpec{Subject: "owner", Method: "flickr.galleries.getList", Params: params})
	if cursor.Page != 1 || cursor.Continuation != "0" || cursor.Params["continuation"] != "" || cursor.Params["user_id"] != "owner" {
		t.Fatalf("cursor = %+v", cursor)
	}
	if params["continuation"] != "0" {
		t.Fatalf("NewCursor mutated spec params: %#v", params)
	}
	encoded, err := json.Marshal(cursor.WithContinuation("opaque:/next?token=a"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseCursor(encoded)
	if err != nil || parsed.Continuation != "opaque:/next?token=a" {
		t.Fatalf("parsed cursor = %+v, %v", parsed, err)
	}
}

func TestWorkSpecAndLaneValidation(t *testing.T) {
	if err := (WorkSpec{Subject: "s", Method: "m"}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []WorkSpec{{Method: "m"}, {Subject: "s"}, {Subject: " ", Method: " "}} {
		if !errors.Is(spec.Validate(), ErrInvalidFacet) {
			t.Fatalf("%+v must be invalid", spec)
		}
	}
	if !LanePerson.Valid() || !LanePhoto.Valid() || Lane("x").Valid() {
		t.Fatal("lane validity wrong")
	}
}
