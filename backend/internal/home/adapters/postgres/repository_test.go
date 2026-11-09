package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type queryerStub struct {
	query string
	args  []any
	row   stubRow
}

func (q *queryerStub) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	q.query = query
	q.args = args
	return q.row
}

type stubRow struct {
	values []any
	err    error
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("wrong destination count")
	}
	for i, value := range r.values {
		switch target := dest[i].(type) {
		case *time.Time:
			*target = value.(time.Time)
		case *int64:
			*target = value.(int64)
		case *[]byte:
			*target = append((*target)[:0], value.([]byte)...)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

func TestNewRequiresDatabase(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
}

func TestLoadRejectsInvalidRepositoryContextAndUser(t *testing.T) {
	for _, test := range []struct {
		name string
		repo *Repository
		ctx  context.Context
		user int64
	}{
		{name: "nil repository", ctx: t.Context(), user: 1},
		{name: "nil context", repo: &Repository{db: &queryerStub{}}, user: 1},
		{name: "invalid user", repo: &Repository{db: &queryerStub{}}, ctx: t.Context()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.repo.Load(test.ctx, test.user); err == nil {
				t.Fatal("Load() succeeded")
			}
		})
	}
}

func TestLoadReturnsUserScopedSnapshot(t *testing.T) {
	generatedAt := time.Date(2026, time.November, 9, 10, 0, 0, 0, time.UTC)
	reader := &queryerStub{row: stubRow{values: []any{
		generatedAt,
		int64(4), int64(1), int64(3),
		int64(12), int64(9), int64(7),
		[]byte(`[{"id":1,"title":"A","thumbnail_url":null,"owner_nsid":"owner","owner_name":"Owner","taken_at":null,"captioned":false}]`),
		[]byte(`[{"nsid":"owner","name":"Owner","icon_url":"","photos_count":12,"sizes_ready":9,"sizes_total":12,"crawl_state":"waiting"}]`),
		[]byte(`[]`),
	}}}
	repository, err := New(reader)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load(t.Context(), 31)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GeneratedAt != generatedAt || snapshot.Contacts.Total != 4 || snapshot.Contacts.Direct != 1 || snapshot.Contacts.Discovered != 3 {
		t.Fatalf("counts or generated_at = %+v", snapshot)
	}
	if snapshot.Photos.Total != 12 || snapshot.Photos.SizesReady != 9 || snapshot.Photos.Captioned != 7 {
		t.Fatalf("photo counts = %+v", snapshot.Photos)
	}
	if len(snapshot.NewPhotos) != 1 || snapshot.NewPhotos[0].ID != 1 || snapshot.NewPhotos[0].ThumbnailURL != nil || snapshot.NewPhotos[0].TakenAt != nil {
		t.Fatalf("new photos = %+v", snapshot.NewPhotos)
	}
	if len(snapshot.StarredContacts) != 1 || snapshot.StarredContacts[0].CrawlState != "waiting" || snapshot.Activity == nil {
		t.Fatalf("starred contacts/activity = %+v / %+v", snapshot.StarredContacts, snapshot.Activity)
	}
	if len(reader.args) != 1 || reader.args[0] != int64(31) {
		t.Fatalf("query args = %#v, want user id 31", reader.args)
	}
	for _, fragment := range []string{"o.user_id = $1", "shared_photo.visibility = 'public'", "photo.visibility = 'public'", "stored.visibility = 'public'", "LIMIT 12", "LIMIT 5", "stored.id = visible.photo_id", "work_items item"} {
		if !strings.Contains(reader.query, fragment) {
			t.Errorf("snapshot query is missing %q", fragment)
		}
	}
}

func TestLoadWrapsDatabaseErrors(t *testing.T) {
	want := errors.New("database unavailable")
	repository, err := New(&queryerStub{row: stubRow{err: want}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load(t.Context(), 1); !errors.Is(err, want) {
		t.Fatalf("Load() err = %v, want wrapped database error", err)
	}
}

func TestLoadReportsInvalidSnapshotJSON(t *testing.T) {
	valid := []byte(`[]`)
	for _, test := range []struct {
		name string
		new  []byte
		star []byte
		act  []byte
	}{
		{name: "new photos", new: []byte(`{`), star: valid, act: valid},
		{name: "starred contacts", new: valid, star: []byte(`{`), act: valid},
		{name: "activity", new: valid, star: valid, act: []byte(`{`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := []any{
				time.Date(2026, time.November, 9, 10, 0, 0, 0, time.UTC), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0),
				test.new, test.star, test.act,
			}
			repository, err := New(&queryerStub{row: stubRow{values: values}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repository.Load(t.Context(), 1); err == nil {
				t.Fatal("Load() succeeded with invalid snapshot JSON")
			}
		})
	}
}
