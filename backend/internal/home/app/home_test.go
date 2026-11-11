package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type contactDepthReader struct {
	counts ContactDepthCounts
	err    error
	userID int64
	calls  int
}

func (r *contactDepthReader) ContactDepths(_ context.Context, userID int64) (ContactDepthCounts, error) {
	r.calls++
	r.userID = userID
	return r.counts, r.err
}

func TestSnapshotCountsContactsAndMarksUnavailableSections(t *testing.T) {
	now := time.Date(2026, 11, 12, 2, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	reader := &contactDepthReader{counts: ContactDepthCounts{Direct: 2, Discovered: 4}}
	service, err := New(reader, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.Snapshot(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if reader.userID != 42 || reader.calls != 1 {
		t.Fatalf("reader scope = user %d, calls %d", reader.userID, reader.calls)
	}
	if got.Contacts != (ContactCounts{Total: 6, Direct: 2, Discovered: 4}) {
		t.Fatalf("contacts = %+v", got.Contacts)
	}
	if got.Photos != (PhotoCounts{}) {
		t.Fatalf("photo metrics = %+v, want unavailable nil values", got.Photos)
	}
	if !got.GeneratedAt.Equal(now.UTC()) || got.GeneratedAt.Location() != time.UTC {
		t.Fatalf("generated_at = %v, want %v UTC", got.GeneratedAt, now.UTC())
	}
	wantUnavailable := []Section{SectionPhotos, SectionStarredContacts, SectionActivity}
	if len(got.UnavailableSections) != len(wantUnavailable) {
		t.Fatalf("unavailable sections = %v", got.UnavailableSections)
	}
	for i, want := range wantUnavailable {
		if got.UnavailableSections[i] != want {
			t.Fatalf("unavailable sections = %v, want %v", got.UnavailableSections, wantUnavailable)
		}
	}
}

func TestSnapshotReturnsReaderError(t *testing.T) {
	readErr := errors.New("database unavailable")
	service, err := New(&contactDepthReader{err: readErr}, func() time.Time { return time.Time{} })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Snapshot(t.Context(), 7); !errors.Is(err, readErr) {
		t.Fatalf("Snapshot() error = %v, want wrapped reader error", err)
	}
}

func TestSnapshotRejectsInvalidUserBeforeReading(t *testing.T) {
	reader := &contactDepthReader{}
	service, err := New(reader, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Snapshot(t.Context(), 0); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("Snapshot() error = %v, want ErrInvalidUser", err)
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls = %d, want 0", reader.calls)
	}
}

func TestNewRequiresDependencies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader ContactDepthReader
		now    func() time.Time
	}{
		{name: "reader", now: time.Now},
		{name: "clock", reader: &contactDepthReader{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.reader, tc.now); err == nil {
				t.Fatal("New() succeeded with a missing dependency")
			}
		})
	}
}
