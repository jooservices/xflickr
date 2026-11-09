package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type snapshotReader struct {
	snapshot Snapshot
	err      error
	userID   int64
	called   bool
}

func (r *snapshotReader) Load(_ context.Context, userID int64) (Snapshot, error) {
	r.called = true
	r.userID = userID
	return r.snapshot, r.err
}

func TestHomeServiceReturnsUserSnapshot(t *testing.T) {
	generated := time.Date(2026, time.November, 9, 10, 0, 0, 0, time.UTC)
	want := Snapshot{GeneratedAt: generated, Contacts: ContactCounts{Total: 3, Direct: 1, Discovered: 2}, NewPhotos: []Photo{}, StarredContacts: []Contact{}, Activity: []Activity{}}
	reader := &snapshotReader{snapshot: want}
	got, err := (Service{Reader: reader}).Get(t.Context(), 8)
	if err != nil || got.GeneratedAt != generated || got.Contacts.Total != 3 || reader.userID != 8 || !reader.called {
		t.Fatalf("Get() = %+v, err = %v, reader = %+v", got, err, reader)
	}
}

func TestHomeServiceRejectsInvalidUserBeforeReading(t *testing.T) {
	reader := &snapshotReader{}
	_, err := (Service{Reader: reader}).Get(t.Context(), 0)
	if !errors.Is(err, ErrInvalidUser) || reader.called {
		t.Fatalf("Get() err = %v, reader called = %t", err, reader.called)
	}
}

func TestHomeServiceRejectsNilContextBeforeReading(t *testing.T) {
	reader := &snapshotReader{}
	_, err := (Service{Reader: reader}).Get(nil, 8)
	if !errors.Is(err, ErrInvalidContext) || reader.called {
		t.Fatalf("Get() err = %v, reader called = %t", err, reader.called)
	}
}

func TestHomeServiceRequiresReader(t *testing.T) {
	_, err := (Service{}).Get(t.Context(), 8)
	if !errors.Is(err, ErrReaderRequired) {
		t.Fatalf("Get() err = %v, want ErrReaderRequired", err)
	}
}

func TestHomeServiceWrapsReaderErrors(t *testing.T) {
	want := errors.New("database unavailable")
	_, err := (Service{Reader: &snapshotReader{err: want}}).Get(t.Context(), 8)
	if !errors.Is(err, want) {
		t.Fatalf("Get() err = %v, want wrapped reader error", err)
	}
}
