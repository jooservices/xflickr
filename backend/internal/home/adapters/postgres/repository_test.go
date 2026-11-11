package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/queue"
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
		case **time.Time:
			*target = value.(*time.Time)
		case **int64:
			if value == nil {
				*target = nil
				break
			}
			switch item := value.(type) {
			case int64:
				*target = &item
			case *int64:
				*target = item
			default:
				return errors.New("unexpected nullable int64 value")
			}
		case *int64:
			*target = value.(int64)
		case *bool:
			*target = value.(bool)
		case *[]byte:
			*target = append((*target)[:0], value.([]byte)...)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

type refreshQueueStub struct {
	job   queue.Job
	opts  queue.Options
	err   error
	calls int
}

func (q *refreshQueueStub) EnqueueStandalone(_ context.Context, job queue.Job, opts queue.Options) error {
	q.job, q.opts = job, opts
	q.calls++
	return q.err
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
	computedAt := generatedAt.Add(-time.Minute)
	reader := &queryerStub{row: stubRow{values: []any{
		generatedAt,
		int64(4), int64(1), int64(3),
		int64(12), int64(9), int64(7),
		&computedAt, false, false,
		[]byte(`[{"id":1,"title":"A","thumbnail_url":null,"owner_nsid":"owner","owner_name":"Owner","taken_at":null,"captioned":false}]`),
		[]byte(`[{"nsid":"owner","name":"Owner","icon_url":"","photos_count":12,"sizes_ready":9,"sizes_total":12,"crawl_state":"waiting"}]`),
		[]byte(`[{"id":"crawl:1","kind":"crawl","message_key":"unknown","count":0,"contact_name":"Owner","occurred_at":"2026-11-09T09:59:00Z","severity":"info"}]`),
	}}}
	repository, err := New(reader)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load(t.Context(), 31)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GeneratedAt != generatedAt || snapshot.CountsComputedAt == nil || *snapshot.CountsComputedAt != computedAt || snapshot.Estimated || snapshot.Contacts.Total != 4 || snapshot.Contacts.Direct != 1 || snapshot.Contacts.Discovered != 3 {
		t.Fatalf("counts or generated_at = %+v", snapshot)
	}
	if snapshot.Photos.Total != 12 || snapshot.Photos.SizesReady != 9 || snapshot.Photos.Captioned != 7 {
		t.Fatalf("photo counts = %+v", snapshot.Photos)
	}
	if len(snapshot.NewPhotos) != 1 || snapshot.NewPhotos[0].ID != 1 || snapshot.NewPhotos[0].ThumbnailURL != nil || snapshot.NewPhotos[0].TakenAt != nil {
		t.Fatalf("new photos = %+v", snapshot.NewPhotos)
	}
	if len(snapshot.StarredContacts) != 1 || snapshot.StarredContacts[0].CrawlState != "waiting" ||
		len(snapshot.Activity) != 1 || snapshot.Activity[0].ID != "crawl:1" ||
		snapshot.Activity[0].Kind != "crawl" || snapshot.Activity[0].ContactName != "Owner" {
		t.Fatalf("starred contacts/activity = %+v / %+v", snapshot.StarredContacts, snapshot.Activity)
	}
	if len(reader.args) != 1 || reader.args[0] != int64(31) {
		t.Fatalf("query args = %#v, want user id 31", reader.args)
	}
	for _, fragment := range []string{
		"user_home_photo_candidates candidate",
		"user_home_shared_photo_candidates shared_candidate",
		"shared_candidate.uploaded_at DESC NULLS LAST",
		"photo.visibility = 'public'",
		"photo.tombstoned_at IS NULL",
		"LIMIT 12",
		"LIMIT 5",
		"user_home_stats",
		"source_revision",
		"shared_revision",
		"work_items item",
		"FROM downloads",
		"FROM api_call_log",
	} {
		if !strings.Contains(reader.query, fragment) {
			t.Errorf("snapshot query is missing %q", fragment)
		}
	}
	ownCandidates := strings.Index(reader.query, "own_photo_candidates AS MATERIALIZED")
	if ownCandidates < 0 {
		t.Fatal("Home query is missing the indexed own-photo candidate source")
	}
	candidateLimit := ownCandidates + strings.Index(reader.query[ownCandidates:], "LIMIT 12")
	photoJoin := strings.Index(reader.query, "JOIN photos photo ON photo.id = candidate.photo_id")
	if candidateLimit < 0 || photoJoin < candidateLimit {
		t.Fatal("Home must cap the indexed candidate sources before joining photos")
	}
	if strings.Contains(reader.query, "visible_photo_counts") || strings.Contains(reader.query, "CROSS JOIN LATERAL (\n        SELECT stored") {
		t.Fatal("request query still builds a photo-wide count or per-photo lateral lookup")
	}
}

func TestLoadInvalidatesCountsWhenSharingPolicyChanges(t *testing.T) {
	generatedAt := time.Date(2026, time.November, 9, 10, 0, 0, 0, time.UTC)
	computedAt := generatedAt.Add(-time.Minute)
	reader := &queryerStub{row: stubRow{values: []any{
		generatedAt,
		int64(4), int64(1), int64(3),
		int64(12), int64(9), int64(7),
		&computedAt, false, false,
		[]byte(`[]`), []byte(`[]`), []byte(`[]`),
	}}}
	repository, err := New(reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load(t.Context(), 31); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reader.query, "invalidated_at") {
		t.Fatal("Home snapshot query does not hide counts invalidated by a sharing change")
	}
}

func TestLoadScansSharedCandidatesInBoundedPermissionCheckedBatches(t *testing.T) {
	generatedAt := time.Date(2026, time.November, 9, 10, 0, 0, 0, time.UTC)
	computedAt := generatedAt.Add(-time.Minute)
	reader := &queryerStub{row: stubRow{values: []any{
		generatedAt,
		int64(4), int64(1), int64(3),
		int64(12), int64(9), int64(7),
		&computedAt, false, false,
		[]byte(`[]`), []byte(`[]`), []byte(`[]`),
	}}}
	repository, err := New(reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load(t.Context(), 31); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"LIMIT 50",
		"batch_number < 4",
		"observations observation",
		"setting.enabled",
	} {
		if !strings.Contains(reader.query, fragment) {
			t.Errorf("shared photo scan is missing %q", fragment)
		}
	}
}

func TestLoadQueuesMissingSnapshotRefresh(t *testing.T) {
	generatedAt := time.Date(2026, time.November, 9, 10, 0, 0, 0, time.UTC)
	reader := &queryerStub{row: stubRow{values: []any{
		generatedAt, int64(0), int64(0), int64(0),
		(*int64)(nil), (*int64)(nil), (*int64)(nil), (*time.Time)(nil), true, true,
		[]byte(`[]`), []byte(`[]`), []byte(`[]`),
	}}}
	refreshQueue := &refreshQueueStub{}
	repository, err := New(reader, refreshQueue)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load(t.Context(), 31)
	if err != nil {
		t.Fatal(err)
	}
	job, err := app.ParseRefreshJob(refreshQueue.job.Args)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CountsComputedAt != nil || !snapshot.Estimated || !snapshot.CountsStale || refreshQueue.calls != 1 || refreshQueue.job.Kind != app.RefreshJobKind || job.UserID != 31 || !refreshQueue.opts.UniqueByArgs || !refreshQueue.opts.UniqueByQueue {
		t.Fatalf("missing snapshot refresh = snapshot %+v, queue %+v", snapshot, refreshQueue)
	}
}

func TestLoadWrapsRefreshEnqueueErrors(t *testing.T) {
	want := errors.New("queue unavailable")
	reader := &queryerStub{row: stubRow{values: []any{
		time.Now(), int64(0), int64(0), int64(0),
		(*int64)(nil), (*int64)(nil), (*int64)(nil), (*time.Time)(nil), true, true,
		[]byte(`[]`), []byte(`[]`), []byte(`[]`),
	}}}
	refreshQueue := &refreshQueueStub{err: want}
	repository, err := New(reader, refreshQueue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load(t.Context(), 31); !errors.Is(err, want) {
		t.Fatalf("Load() error = %v, want wrapped enqueue error", err)
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
				time.Date(2026, time.November, 9, 10, 0, 0, 0, time.UTC), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), (*time.Time)(nil), false,
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
