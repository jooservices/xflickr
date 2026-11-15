package app

import (
	"context"
	"errors"
	"testing"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
)

type sizesContinuationFake struct {
	calls int
	event catalogapp.SizesResolved
	err   error
}

func (f *sizesContinuationFake) ResolveSizes(_ context.Context, id, userID int64, status string) error {
	f.calls++
	f.event = catalogapp.SizesResolved{PhotoID: id, UserID: userID, Status: status}
	return f.err
}

func TestSizesResolvedHandler(t *testing.T) {
	for _, status := range []string{"confirmed", "unavailable"} {
		repo := &sizesContinuationFake{}
		if err := (SizesResolvedHandler{Repository: repo}).Handle(t.Context(), []byte(`{"photo_id":42,"user_id":7,"status":"`+status+`"}`)); err != nil {
			t.Fatal(err)
		}
		if repo.calls != 1 || repo.event.PhotoID != 42 || repo.event.UserID != 7 || repo.event.Status != status {
			t.Fatalf("event=%+v", repo)
		}
	}
	repo := &sizesContinuationFake{err: errors.New("database")}
	h := SizesResolvedHandler{Repository: repo}
	for _, raw := range []string{`{`, `{}`, `{"photo_id":0,"status":"confirmed"}`, `{"photo_id":1,"status":"none"}`, `{"photo_id":1,"user_id":-1,"status":"confirmed"}`} {
		if err := h.Handle(t.Context(), []byte(raw)); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
	if repo.calls != 0 {
		t.Fatal("invalid event reached repository")
	}
	if err := h.Handle(t.Context(), []byte(`{"photo_id":1,"status":"confirmed"}`)); !errors.Is(err, repo.err) {
		t.Fatal(err)
	}
	if err := (SizesResolvedHandler{}).Handle(t.Context(), nil); err == nil {
		t.Fatal("missing repository accepted")
	}
	if err := h.Handle(nil, nil); err == nil {
		t.Fatal("nil context accepted")
	}
}
