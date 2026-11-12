package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

type photosFixture struct {
	ctx   context.Context
	pool  *pgxpool.Pool
	repo  *Repository
	owner string
	ids   []int64
	now   time.Time
}

func openPhotosFixture(t *testing.T, count int) photosFixture {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	pool := dbtest.Open(t, loadFavoritesDatabase(t))
	repo, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UTC().UnixNano()
	owner := fmt.Sprintf("photos-upsert-%d@example.invalid", suffix)
	ids := make([]int64, count)
	for i := range ids {
		ids[i] = suffix + int64(i)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		_, _ = pool.Exec(cleanup, `DELETE FROM photos WHERE id=ANY($1)`, ids)
		_, _ = pool.Exec(cleanup, `DELETE FROM people WHERE nsid=$1`, owner)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, 'photos-upsert')`, owner); err != nil {
		t.Fatal(err)
	}
	return photosFixture{ctx: ctx, pool: pool, repo: repo, owner: owner, ids: ids, now: time.Now().UTC().Truncate(time.Microsecond)}
}

func (f photosFixture) photos(title string, ids []int64) []catalog.Photo {
	out := make([]catalog.Photo, len(ids))
	for i, id := range ids {
		out[i] = catalog.Photo{
			ID: id, OwnerNSID: f.owner, Title: title, LastUpdate: f.now, Visibility: catalog.VisibilityPublic,
			Raw: []byte(fmt.Sprintf(`{"id":%d,"title":%q}`, id, title)),
		}
	}
	return out
}

func (f photosFixture) upsert(t *testing.T, photos []catalog.Photo) {
	t.Helper()
	for start := 0; start < len(photos); start += 500 {
		end := min(start+500, len(photos))
		if err := f.repo.UpsertPhotos(f.ctx, photos[start:end]); err != nil {
			t.Fatalf("upsert photos: %v", err)
		}
	}
}

func (f photosFixture) versions(t *testing.T, ids []int64) map[int64]string {
	t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT id, xmin::text||'/'||ctid::text FROM photos WHERE id=ANY($1)`, ids)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var version string
		if err := rows.Scan(&id, &version); err != nil {
			t.Fatal(err)
		}
		out[id] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPhotosSamePayloadKeepsRowVersion(t *testing.T) {
	f := openPhotosFixture(t, 1000)
	photos := f.photos("same", f.ids)
	f.upsert(t, photos)
	before := f.versions(t, f.ids)
	if len(before) != len(f.ids) {
		t.Fatalf("inserted %d rows, want %d", len(before), len(f.ids))
	}
	f.upsert(t, photos)
	after := f.versions(t, f.ids)
	for _, id := range f.ids {
		if after[id] != before[id] {
			t.Fatalf("photo %d was rewritten: %s -> %s", id, before[id], after[id])
		}
	}
}

func TestPhotosChangedRowsAreUpdatedAndStaleSeenAtIsRefreshed(t *testing.T) {
	f := openPhotosFixture(t, 3)
	f.upsert(t, f.photos("first", f.ids))
	before := f.versions(t, f.ids)

	changed := f.photos("second", f.ids[:1])
	changed[0].LastUpdate = f.now.Add(time.Minute)
	f.upsert(t, changed)
	var title string
	if err := f.pool.QueryRow(f.ctx, `SELECT title FROM photos WHERE id=$1`, f.ids[0]).Scan(&title); err != nil || title != "second" {
		t.Fatalf("changed photo title = %q, err=%v", title, err)
	}
	if f.versions(t, f.ids[:1])[f.ids[0]] == before[f.ids[0]] {
		t.Fatal("changed photo was not updated")
	}

	if _, err := f.pool.Exec(f.ctx, `UPDATE photos SET last_seen_at = now() - interval '2 hours' WHERE id=$1`, f.ids[1]); err != nil {
		t.Fatal(err)
	}
	stale := f.versions(t, f.ids[1:2])[f.ids[1]]
	f.upsert(t, f.photos("first", f.ids[1:2]))
	var fresh bool
	if err := f.pool.QueryRow(f.ctx, `SELECT last_seen_at > now() - interval '5 minutes' FROM photos WHERE id=$1`, f.ids[1]).Scan(&fresh); err != nil || !fresh {
		t.Fatalf("stale last_seen_at was not refreshed: fresh=%v err=%v", fresh, err)
	}
	if f.versions(t, f.ids[1:2])[f.ids[1]] == stale {
		t.Fatal("stale photo row was not updated")
	}

	tombstone := f.now
	gone := f.photos("first", f.ids[2:3])
	gone[0].TombstonedAt = &tombstone
	f.upsert(t, gone)
	var tombstoned bool
	if err := f.pool.QueryRow(f.ctx, `SELECT tombstoned_at IS NOT NULL FROM photos WHERE id=$1`, f.ids[2]).Scan(&tombstoned); err != nil || !tombstoned {
		t.Fatalf("tombstone was not stored: %v err=%v", tombstoned, err)
	}
}

func TestPhotosDuplicateIDsInOneBatchKeepTheLastPayload(t *testing.T) {
	f := openPhotosFixture(t, 1)
	first, last := f.photos("first", f.ids), f.photos("last", f.ids)
	last[0].LastUpdate = f.now.Add(time.Minute)
	if err := f.repo.UpsertPhotos(f.ctx, append(first, last...)); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := f.pool.QueryRow(f.ctx, `SELECT title FROM photos WHERE id=$1`, f.ids[0]).Scan(&title); err != nil || title != "last" {
		t.Fatalf("title = %q, err=%v", title, err)
	}
}

func TestPhotosConcurrentOverlappingBatchesDoNotDeadlock(t *testing.T) {
	f := openPhotosFixture(t, 400)
	reversed := make([]int64, len(f.ids))
	for i, id := range f.ids {
		reversed[len(f.ids)-1-i] = id
	}
	forward, backward := f.photos("a", f.ids), f.photos("b", reversed)
	for i := range backward {
		backward[i].LastUpdate = f.now.Add(time.Minute)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for round := 0; round < 4; round++ {
		for _, batch := range [][]catalog.Photo{forward, backward} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := f.repo.UpsertPhotos(f.ctx, batch); err != nil {
					errs <- err
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent photo upsert failed: %v", err)
	}
}

func TestPhotosUpdateUsesPrimaryKeyIndex(t *testing.T) {
	f := openPhotosFixture(t, 1)
	f.upsert(t, f.photos("plan", f.ids))
	rows, err := f.pool.Query(f.ctx, `EXPLAIN (FORMAT TEXT) `+photoUpdateSQL,
		f.ids[0], f.owner, "", "", "plan", "", []string{}, nil, nil, f.now, "public", "", "", []byte(`{}`), []byte(`{}`), make([]byte, 32), nil, f.now)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan += line + "\n"
	}
	if !strings.Contains(plan, "Index Scan using photos_p") || !strings.Contains(plan, "_pkey") || strings.Contains(plan, "Seq Scan") {
		t.Fatalf("photo update must use a partition primary key index without a Seq Scan:\n%s", plan)
	}
}
