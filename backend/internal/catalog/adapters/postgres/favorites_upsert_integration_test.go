package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestFavoritesSamePayloadKeepsRowVersion(t *testing.T) {
	ctx, pool, repository, person, ids, now := openFavoritesFixture(t, 1000)
	favorites := make([]Favorite, len(ids))
	for i, id := range ids {
		favorites[i] = Favorite{PersonNSID: person, PhotoID: id, FaveDate: now.Add(time.Duration(i) * time.Microsecond), SeenAt: now}
	}
	if err := upsertFavoritesInChunks(ctx, repository, favorites); err != nil {
		t.Fatalf("insert favorites: %v", err)
	}
	before, err := readFavoriteVersions(ctx, pool, person, ids)
	if err != nil {
		t.Fatalf("read initial favorite versions: %v", err)
	}
	if len(before) != len(ids) {
		t.Fatalf("initial favorite rows = %d, want %d", len(before), len(ids))
	}
	if err := upsertFavoritesInChunks(ctx, repository, favorites); err != nil {
		t.Fatalf("rewrite unchanged favorites: %v", err)
	}
	after, err := readFavoriteVersions(ctx, pool, person, ids)
	if err != nil {
		t.Fatalf("read rewritten favorite versions: %v", err)
	}
	for _, id := range ids {
		if after[id] != before[id] {
			t.Errorf("favorite %d version changed from %+v to %+v", id, before[id], after[id])
		}
	}
}

func TestFavoritesCrawlUpsertRefreshesSeenAt(t *testing.T) {
	ctx, pool, repository, person, ids, now := openFavoritesFixture(t, 1)
	id := ids[0]
	initial := Favorite{PersonNSID: person, PhotoID: id, FaveDate: now, SeenAt: now.Add(-time.Hour)}
	if err := repository.UpsertFavoritesBatch(ctx, []Favorite{initial}); err != nil {
		t.Fatalf("insert initial favorite: %v", err)
	}
	before, err := readFavoriteVersions(ctx, pool, person, ids)
	if err != nil {
		t.Fatalf("read initial favorite version: %v", err)
	}

	if err := repository.UpsertFavorites(ctx, []catalog.Favorite{{PersonNSID: person, PhotoID: id, FaveDate: now}}); err != nil {
		t.Fatalf("upsert crawl favorite: %v", err)
	}
	after, err := readFavoriteVersions(ctx, pool, person, ids)
	if err != nil {
		t.Fatalf("read crawl favorite version: %v", err)
	}
	if after[id] == before[id] {
		t.Fatal("crawl favorite row version did not change when SeenAt was omitted")
	}
	var seen time.Time
	if err := pool.QueryRow(ctx, `SELECT seen_at FROM favorites WHERE person_nsid=$1 AND photo_id=$2`, person, id).Scan(&seen); err != nil {
		t.Fatalf("read crawl favorite seen_at: %v", err)
	}
	if !seen.After(initial.SeenAt) {
		t.Fatalf("crawl favorite seen_at = %s, want later than %s", seen, initial.SeenAt)
	}
}

func TestFavoritesUnchangedDoesNotWaitForRowLock(t *testing.T) {
	ctx, pool, repository, person, ids, now := openFavoritesFixture(t, 1)
	favorite := Favorite{PersonNSID: person, PhotoID: ids[0], FaveDate: now, SeenAt: now}
	if err := repository.UpsertFavoritesBatch(ctx, []Favorite{favorite}); err != nil {
		t.Fatalf("insert favorite: %v", err)
	}

	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Rollback(context.Background()) })
	var lockedID int64
	if err := blocker.QueryRow(ctx, `SELECT photo_id FROM favorites WHERE person_nsid=$1 AND photo_id=$2 FOR UPDATE`, person, ids[0]).Scan(&lockedID); err != nil {
		t.Fatalf("lock favorite row: %v", err)
	}

	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Rollback(context.Background()) })
	if _, err := writer.Exec(ctx, `SET LOCAL lock_timeout = '500ms'`); err != nil {
		t.Fatalf("set writer lock timeout: %v", err)
	}
	writerRepository, err := New(writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := writerRepository.UpsertFavoritesBatch(ctx, []Favorite{favorite}); err != nil {
		t.Fatalf("unchanged favorite waited for a row lock: %v", err)
	}
}

func TestFavoritesChangedAndRestored(t *testing.T) {
	ctx, pool, repository, person, ids, now := openFavoritesFixture(t, 1)
	id := ids[0]
	initial := Favorite{PersonNSID: person, PhotoID: id, FaveDate: now.Add(-time.Hour), SeenAt: now}
	if err := repository.UpsertFavoritesBatch(ctx, []Favorite{initial}); err != nil {
		t.Fatalf("insert initial favorite: %v", err)
	}

	changedDate := initial
	changedDate.FaveDate = now
	if err := repository.UpsertFavoritesBatch(ctx, []Favorite{changedDate}); err != nil {
		t.Fatalf("change favorite date: %v", err)
	}
	assertFavoriteTimes(t, ctx, pool, person, id, now, now)

	advancedSeenAt := changedDate
	advancedSeenAt.SeenAt = now.Add(time.Hour)
	if err := repository.UpsertFavoritesBatch(ctx, []Favorite{advancedSeenAt}); err != nil {
		t.Fatalf("advance seen_at: %v", err)
	}
	assertFavoriteTimes(t, ctx, pool, person, id, now, now.Add(time.Hour))

	olderSeenAt := advancedSeenAt
	olderSeenAt.SeenAt = now.Add(-2 * time.Hour)
	if err := repository.UpsertFavoritesBatch(ctx, []Favorite{olderSeenAt}); err != nil {
		t.Fatalf("store older seen_at: %v", err)
	}
	assertFavoriteTimes(t, ctx, pool, person, id, now, now.Add(-2*time.Hour))

	if _, err := pool.Exec(ctx, `UPDATE favorites SET removed_at=$3 WHERE person_nsid=$1 AND photo_id=$2`, person, id, now); err != nil {
		t.Fatalf("mark favorite removed: %v", err)
	}
	firstDuplicate := olderSeenAt
	firstDuplicate.FaveDate = now.Add(2 * time.Hour)
	lastDuplicate := olderSeenAt
	lastDuplicate.FaveDate = now.Add(3 * time.Hour)
	lastDuplicate.SeenAt = now.Add(4 * time.Hour)
	if err := repository.UpsertFavoritesBatch(ctx, []Favorite{firstDuplicate, lastDuplicate}); err != nil {
		t.Fatalf("restore duplicate favorite: %v", err)
	}
	assertFavoriteTimes(t, ctx, pool, person, id, lastDuplicate.FaveDate, lastDuplicate.SeenAt)
	var removedAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT removed_at FROM favorites WHERE person_nsid=$1 AND photo_id=$2`, person, id).Scan(&removedAt); err != nil {
		t.Fatalf("read restored favorite: %v", err)
	}
	if removedAt.Valid {
		t.Fatalf("restored favorite removed_at = %s, want NULL", removedAt.Time)
	}
}

func TestFavoritesBatchRollbackOnError(t *testing.T) {
	ctx, pool, repository, person, ids, now := openFavoritesFixture(t, 1)
	missingPhotoID := ids[0] + 1
	batch := []Favorite{
		{PersonNSID: person, PhotoID: ids[0], FaveDate: now, SeenAt: now},
		{PersonNSID: person, PhotoID: missingPhotoID, FaveDate: now, SeenAt: now},
	}
	if err := repository.UpsertFavoritesBatch(ctx, batch); err == nil {
		t.Fatal("batch with a missing photo succeeded")
	}
	assertNoFavorites(t, ctx, pool, person, []int64{ids[0], missingPhotoID})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	txRepository, err := New(tx)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := txRepository.UpsertFavoritesBatch(ctx, batch[:1]); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("write caller transaction: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback caller transaction: %v", err)
	}
	assertNoFavorites(t, ctx, pool, person, []int64{ids[0]})
}

func TestFavoritesUpdatePlanUsesPrimaryKey(t *testing.T) {
	ctx, pool, repository, person, ids, now := openFavoritesFixture(t, favoritesBatchMax*2)
	favorites := make([]Favorite, len(ids))
	for i, id := range ids {
		favorites[i] = Favorite{PersonNSID: person, PhotoID: id, FaveDate: now, SeenAt: now}
	}
	if err := upsertFavoritesInChunks(ctx, repository, favorites); err != nil {
		t.Fatalf("seed favorite update plan: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE favorites`); err != nil {
		t.Fatalf("analyze favorite plan fixture: %v", err)
	}
	newFaveDate := now.Add(time.Hour)
	newSeenAt := now.Add(2 * time.Hour)
	var raw []byte
	if err := pool.QueryRow(ctx, `EXPLAIN (FORMAT JSON) `+favoriteUpdateSQL, person, ids[0], newFaveDate, newSeenAt).Scan(&raw); err != nil {
		t.Fatalf("explain favorite update: %v", err)
	}
	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode favorite update plan: %v", err)
	}
	hasSeqScan, indexNames := inspectFavoriteUpdatePlan(plan)
	if hasSeqScan {
		t.Fatal("favorite update plan contains a Seq Scan or Parallel Seq Scan")
	}
	for _, name := range indexNames {
		if strings.HasPrefix(name, "favorites_p") && strings.HasSuffix(name, "_pkey") {
			return
		}
	}
	t.Fatalf("favorite update plan does not use a partition primary-key index: %s", raw)
}

func openFavoritesFixture(t *testing.T, count int) (context.Context, *pgxpool.Pool, *Repository, string, []int64, time.Time) {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	pool := dbtest.Open(t, loadFavoritesDatabase(t))
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UTC().UnixNano()
	person := fmt.Sprintf("favorites-%d@example.invalid", suffix)
	ids := make([]int64, count)
	for i := range ids {
		ids[i] = suffix + int64(i)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM favorites WHERE person_nsid=$1`, person); err != nil {
			t.Errorf("delete fixture favorites: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM photos WHERE id=ANY($1)`, ids); err != nil {
			t.Errorf("delete fixture photos: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM people WHERE nsid=$1`, person); err != nil {
			t.Errorf("delete fixture person: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, $2)`, person, "favorites-integration"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO photos (id, owner_nsid, last_update, visibility, raw, content_hash)
		SELECT $1::bigint + generated.i::bigint, $2, $3, 'public', $4, $5
		FROM generate_series(0, $6::integer - 1) AS generated(i)`, suffix, person, now, []byte(`{}`), make([]byte, 32), count); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, repository, person, ids, now
}

func loadFavoritesDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate favorites integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load worktree database settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbConfig
}

func upsertFavoritesInChunks(ctx context.Context, repository *Repository, favorites []Favorite) error {
	for start := 0; start < len(favorites); start += favoritesBatchMax {
		end := min(start+favoritesBatchMax, len(favorites))
		if err := repository.UpsertFavoritesBatch(ctx, favorites[start:end]); err != nil {
			return err
		}
	}
	return nil
}

type favoriteRowVersion struct {
	xmin string
	ctid string
}

func readFavoriteVersions(ctx context.Context, pool *pgxpool.Pool, person string, ids []int64) (map[int64]favoriteRowVersion, error) {
	versions := make(map[int64]favoriteRowVersion, len(ids))
	for start := 0; start < len(ids); start += favoritesBatchMax {
		end := min(start+favoritesBatchMax, len(ids))
		rows, err := pool.Query(ctx, `SELECT photo_id, xmin::text, ctid::text FROM favorites WHERE person_nsid=$1 AND photo_id=ANY($2)`, person, ids[start:end])
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var version favoriteRowVersion
			if err := rows.Scan(&id, &version.xmin, &version.ctid); err != nil {
				rows.Close()
				return nil, err
			}
			versions[id] = version
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return versions, nil
}

func assertFavoriteTimes(t *testing.T, ctx context.Context, pool *pgxpool.Pool, person string, id int64, faveDate, seenAt time.Time) {
	t.Helper()
	var gotFaveDate, gotSeenAt time.Time
	if err := pool.QueryRow(ctx, `SELECT fave_date, seen_at FROM favorites WHERE person_nsid=$1 AND photo_id=$2`, person, id).Scan(&gotFaveDate, &gotSeenAt); err != nil {
		t.Fatalf("read favorite timestamps: %v", err)
	}
	if !gotFaveDate.Equal(faveDate) || !gotSeenAt.Equal(seenAt) {
		t.Fatalf("favorite timestamps = (%s, %s), want (%s, %s)", gotFaveDate, gotSeenAt, faveDate, seenAt)
	}
}

func assertNoFavorites(t *testing.T, ctx context.Context, pool *pgxpool.Pool, person string, ids []int64) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM favorites WHERE person_nsid=$1 AND photo_id=ANY($2))`, person, ids).Scan(&exists); err != nil {
		t.Fatalf("query favorites after rollback: %v", err)
	}
	if exists {
		t.Fatal("rollback left favorite rows")
	}
}

func inspectFavoriteUpdatePlan(value any) (bool, []string) {
	hasSeqScan := false
	var indexes []string
	switch node := value.(type) {
	case map[string]any:
		if nodeType, ok := node["Node Type"].(string); ok && (nodeType == "Seq Scan" || nodeType == "Parallel Seq Scan") {
			hasSeqScan = true
		}
		if indexName, ok := node["Index Name"].(string); ok {
			indexes = append(indexes, indexName)
		}
		for _, child := range node {
			childSeqScan, childIndexes := inspectFavoriteUpdatePlan(child)
			hasSeqScan = hasSeqScan || childSeqScan
			indexes = append(indexes, childIndexes...)
		}
	case []any:
		for _, child := range node {
			childSeqScan, childIndexes := inspectFavoriteUpdatePlan(child)
			hasSeqScan = hasSeqScan || childSeqScan
			indexes = append(indexes, childIndexes...)
		}
	}
	return hasSeqScan, indexes
}

func TestFavoritesConcurrentOverlappingBatches(t *testing.T) {
	ctx, pool, _, person, ids, now := openFavoritesFixture(t, 2)
	concurrentCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx1, err := pool.Begin(concurrentCtx)
	if err != nil {
		t.Fatal(err)
	}
	tx2, err := pool.Begin(concurrentCtx)
	if err != nil {
		_ = tx1.Rollback(context.Background())
		t.Fatal(err)
	}
	defer func() {
		_ = tx1.Rollback(context.Background())
		_ = tx2.Rollback(context.Background())
	}()
	repo1, err := New(tx1)
	if err != nil {
		t.Fatal(err)
	}
	repo2, err := New(tx2)
	if err != nil {
		t.Fatal(err)
	}
	first := []Favorite{{PersonNSID: person, PhotoID: ids[0], FaveDate: now, SeenAt: now}, {PersonNSID: person, PhotoID: ids[1], FaveDate: now.Add(time.Second), SeenAt: now}}
	second := []Favorite{first[1], first[0]}
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	done := make(chan error, 2)
	for _, operation := range []struct {
		repository *Repository
		tx         pgx.Tx
		favorites  []Favorite
	}{{repository: repo1, tx: tx1, favorites: first}, {repository: repo2, tx: tx2, favorites: second}} {
		operation := operation
		go func() {
			ready <- struct{}{}
			<-start
			if err := operation.repository.UpsertFavoritesBatch(concurrentCtx, operation.favorites); err != nil {
				_ = operation.tx.Rollback(context.Background())
				done <- err
				return
			}
			done <- operation.tx.Commit(concurrentCtx)
		}()
	}
	<-ready
	<-ready
	close(start)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("concurrent transaction %d: %v", i+1, err)
			}
		case <-concurrentCtx.Done():
			t.Fatalf("concurrent favorite batches did not commit: %v", concurrentCtx.Err())
		}
	}
	for _, id := range ids {
		want := now
		if id == ids[1] {
			want = now.Add(time.Second)
		}
		assertFavoriteTimes(t, ctx, pool, person, id, want, now)
	}
}
