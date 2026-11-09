package postgres_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestUpsertPeopleConcurrentBatchesDoNotDeadlockOrWaitLong(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("people-lock-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid LIKE $1`, suffix+"%")
	})
	left := make([]domain.Person, 0, 40)
	right := make([]domain.Person, 0, 40)
	for i := 0; i < 40; i++ {
		nsid := fmt.Sprintf("%s-%02d", suffix, i)
		left = append(left, domain.Person{NSID: nsid, Username: nsid})
		right = append(right, domain.Person{NSID: nsid, Username: nsid + "-b"})
	}
	for i, j := 0, len(right)-1; i < j; i, j = i+1, j-1 {
		right[i], right[j] = right[j], right[i]
	}
	repoA, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	repoB, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	start := time.Now()
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- repoA.UpsertPeople(ctx, left)
	}()
	go func() {
		defer wg.Done()
		errs <- repoB.UpsertPeople(ctx, right)
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("UpsertPeople concurrent: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("concurrent upserts took %v, want < 5s", elapsed)
	}
}

func TestNoOpPeopleUpsertCreatesNoTuple(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	ctx := context.Background()
	suffix := fmt.Sprintf("people-noop-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, suffix)
	})
	repo, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	person := domain.Person{NSID: suffix, Username: "noop", Realname: "No Op"}
	if err := repo.UpsertPeople(ctx, []domain.Person{person}); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := pool.QueryRow(ctx, `SELECT xmin::text::bigint FROM people WHERE nsid=$1`, suffix).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertPeople(ctx, []domain.Person{person}); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := pool.QueryRow(ctx, `SELECT xmin::text::bigint FROM people WHERE nsid=$1`, suffix).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("no-op upsert created a new tuple: xmin %d -> %d", before, after)
	}
}

func TestEnsurePeopleNeverLocksExistingRows(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("ensure-people-%d", time.Now().UnixNano())
	cleanupPeople(t, pool, prefix)
	people := make([]domain.Person, 200)
	for i := range people {
		nsid := fmt.Sprintf("%s-%03d", prefix, i)
		people[i] = domain.Person{NSID: nsid, Username: nsid}
	}
	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(ctx, people); err != nil {
		t.Fatalf("seed people: %v", err)
	}
	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = txA.Rollback(context.Background()) })
	repositoryA, err := postgres.New(txA)
	if err != nil {
		t.Fatal(err)
	}
	if err := repositoryA.UpsertPeople(ctx, people); err != nil {
		t.Fatalf("transaction A ensure people: %v", err)
	}
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ctxB, cancelB := context.WithTimeout(ctx, 2*time.Second)
	done := make(chan error, 1)
	go func() {
		repositoryB, repoErr := postgres.New(txB)
		if repoErr == nil {
			repoErr = repositoryB.UpsertPeople(ctxB, people)
		}
		if repoErr == nil {
			repoErr = txB.Commit(ctxB)
		} else {
			_ = txB.Rollback(context.Background())
		}
		done <- repoErr
	}()
	t.Cleanup(func() {
		cancelB()
		_ = txB.Rollback(context.Background())
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("transaction B did not stop after cleanup")
		}
	})
	started := time.Now()
	select {
	case err := <-done:
		cancelB()
		if err != nil {
			t.Fatalf("transaction B ensure people: %v", err)
		}
		if elapsed := time.Since(started); elapsed >= time.Second {
			t.Fatalf("transaction B took %v, want less than 1s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatalf("transaction B waited on unchanged people for at least 1s")
	}
	var locked int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM people WHERE nsid = ANY($1::text[]) AND xmax <> 0`, personNSIDs(people)).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	if locked != 0 {
		t.Fatalf("existing rows have nonzero xmax on %d people, want no row locks", locked)
	}
	var tupleLocks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='tuple' AND relation='people'::regclass`).Scan(&tupleLocks); err != nil {
		t.Fatal(err)
	}
	if tupleLocks != 0 {
		t.Fatalf("pg_locks shows %d tuple locks on people, want 0", tupleLocks)
	}
}

func TestRefreshChangedPeopleLocksOnlyChangedRowsInOrder(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("refresh-people-%d", time.Now().UnixNano())
	cleanupPeople(t, pool, prefix)
	people := []domain.Person{
		{NSID: prefix + "-a", Username: prefix + "-a", Realname: "same"},
		{NSID: prefix + "-b", Username: prefix + "-b", Realname: "old-b"},
		{NSID: prefix + "-c", Username: prefix + "-c", Realname: "old-c"},
	}
	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(ctx, people); err != nil {
		t.Fatalf("seed people: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	repositoryTx, err := postgres.New(tx)
	if err != nil {
		t.Fatal(err)
	}
	incoming := []domain.Person{
		{NSID: people[2].NSID, Username: people[2].Username, Realname: "new-c"},
		{NSID: people[1].NSID, Username: people[1].Username, Realname: "new-b"},
		people[0],
	}
	if err := repositoryTx.UpsertPeople(ctx, incoming); err != nil {
		t.Fatalf("refresh people: %v", err)
	}
	var unchangedXmax int64
	if err := pool.QueryRow(ctx, `SELECT xmax::text::bigint FROM people WHERE nsid=$1`, people[0].NSID).Scan(&unchangedXmax); err != nil {
		t.Fatal(err)
	}
	if unchangedXmax != 0 {
		t.Fatalf("unchanged row xmax = %d, want 0", unchangedXmax)
	}
	var changedRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM people WHERE nsid=ANY($1::text[]) AND xmax <> 0`, []string{people[1].NSID, people[2].NSID}).Scan(&changedRows); err != nil {
		t.Fatal(err)
	}
	if changedRows != 2 {
		t.Fatalf("rows with a pending change = %d, want 2", changedRows)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPlaceholderUsernameDoesNotOverwriteRealOne(t *testing.T) {
	pool, ctx, cancel, repository, prefix := newPeopleIntegration(t, "placeholder")
	defer cancel()
	nsid := prefix + "-person"
	if err := repository.UpsertPeople(ctx, []domain.Person{{NSID: nsid, Username: "real-user"}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(ctx, []domain.Person{{NSID: nsid, Username: nsid}}); err != nil {
		t.Fatal(err)
	}
	var username string
	if err := pool.QueryRow(ctx, `SELECT username FROM people WHERE nsid=$1`, nsid).Scan(&username); err != nil {
		t.Fatal(err)
	}
	if username != "real-user" {
		t.Fatalf("username = %q, want real-user", username)
	}
}

func TestEmptyValuesDoNotEraseData(t *testing.T) {
	pool, ctx, cancel, repository, prefix := newPeopleIntegration(t, "empty-values")
	defer cancel()
	nsid := prefix + "-person"
	original := domain.Person{
		NSID: nsid, Username: "real-user", Realname: "Real Name", Profile: []byte(`{"location":"earth"}`),
		Icon: "icon.jpg", PhotosCount: 42, FacetState: []byte(`{"sizes":"confirmed"}`),
	}
	if err := repository.UpsertPeople(ctx, []domain.Person{original}); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(ctx, []domain.Person{{
		NSID: nsid, Username: nsid, Profile: []byte(`{}`), FacetState: []byte(`{}`),
	}}); err != nil {
		t.Fatal(err)
	}
	var got domain.Person
	if err := pool.QueryRow(ctx, `SELECT username, realname, profile, icon, photos_count, facet_state FROM people WHERE nsid=$1`, nsid).
		Scan(&got.Username, &got.Realname, &got.Profile, &got.Icon, &got.PhotosCount, &got.FacetState); err != nil {
		t.Fatal(err)
	}
	if got.Username != original.Username || got.Realname != original.Realname || string(got.Profile) != string(original.Profile) || got.Icon != original.Icon || got.PhotosCount != original.PhotosCount || string(got.FacetState) != string(original.FacetState) {
		t.Fatalf("empty values erased saved data: %+v", got)
	}
}

func TestLastSeenAdvancesAtMostOncePerHour(t *testing.T) {
	pool, ctx, cancel, repository, prefix := newPeopleIntegration(t, "last-seen")
	defer cancel()
	nsid := prefix + "-person"
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-4 * time.Hour)
	if err := repository.UpsertPeople(ctx, []domain.Person{{NSID: nsid, Username: "real-user", LastSeenAt: &base}}); err != nil {
		t.Fatal(err)
	}
	refresh := func(at time.Time) {
		t.Helper()
		if err := repository.UpsertPeople(ctx, []domain.Person{{NSID: nsid, Username: "real-user", LastSeenAt: &at}}); err != nil {
			t.Fatal(err)
		}
	}
	assertLastSeen := func(want time.Time) {
		t.Helper()
		var got time.Time
		if err := pool.QueryRow(ctx, `SELECT last_seen_at FROM people WHERE nsid=$1`, nsid).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if !got.Equal(want) {
			t.Fatalf("last_seen_at = %s, want %s", got, want)
		}
	}
	refresh(base.Add(30 * time.Minute))
	assertLastSeen(base)
	refresh(base.Add(time.Hour))
	assertLastSeen(base.Add(time.Hour))
	refresh(base.Add(90 * time.Minute))
	assertLastSeen(base.Add(time.Hour))
	refresh(base.Add(2 * time.Hour))
	assertLastSeen(base.Add(2 * time.Hour))
}

func newPeopleIntegration(t *testing.T, kind string) (*pgxpool.Pool, context.Context, context.CancelFunc, *postgres.Repository, string) {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	prefix := fmt.Sprintf("%s-%d", kind, time.Now().UnixNano())
	cleanupPeople(t, pool, prefix)
	repository, err := postgres.New(pool)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return pool, ctx, cancel, repository, prefix
}

func cleanupPeople(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM people WHERE nsid LIKE $1`, prefix+"%"); err != nil {
			t.Errorf("clean up people: %v", err)
		}
	})
}

func personNSIDs(people []domain.Person) []string {
	nsids := make([]string, len(people))
	for i, person := range people {
		nsids[i] = person.NSID
	}
	return nsids
}
