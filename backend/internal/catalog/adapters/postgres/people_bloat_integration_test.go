package postgres_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	if err := repositoryA.EnsurePeople(ctx, people); err != nil {
		t.Fatalf("transaction A ensure people: %v", err)
	}
	var backendPID int32
	if err := txA.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backendPID); err != nil {
		t.Fatal(err)
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
			repoErr = repositoryB.EnsurePeople(ctxB, people)
		}
		if repoErr == nil {
			repoErr = txB.Commit(ctxB)
		} else {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer rollbackCancel()
			_ = txB.Rollback(rollbackCtx)
		}
		done <- repoErr
	}()
	completed := false
	t.Cleanup(func() {
		cancelB()
		if completed {
			return
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("transaction B did not stop after cleanup")
		}
	})
	started := time.Now()
	select {
	case err := <-done:
		completed = true
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
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='tuple' AND relation='people'::regclass AND pid=$1`, backendPID).Scan(&tupleLocks); err != nil {
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
	if err := repositoryTx.RefreshChangedPeople(ctx, incoming); err != nil {
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

	ordered := []domain.Person{
		{NSID: prefix + "-order-a", Username: prefix + "-order-a", Realname: "before-a"},
		{NSID: prefix + "-order-b", Username: prefix + "-order-b", Realname: "before-b"},
	}
	if err := repository.UpsertPeople(ctx, ordered); err != nil {
		t.Fatalf("seed lock-order people: %v", err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := blocker.QueryRow(ctx, `SELECT nsid FROM people WHERE nsid=$1 FOR UPDATE`, ordered[0].NSID).Scan(new(string)); err != nil {
		t.Fatal(err)
	}
	worker, err := pool.Begin(ctx)
	if err != nil {
		_ = blocker.Rollback(context.Background())
		t.Fatal(err)
	}
	applicationName := fmt.Sprintf("xflickr_refresh_order_%d", time.Now().UnixNano())
	if _, err := worker.Exec(ctx, `SELECT set_config('application_name',$1,true)`, applicationName); err != nil {
		_ = blocker.Rollback(context.Background())
		_ = worker.Rollback(context.Background())
		t.Fatal(err)
	}
	workerRepository, err := postgres.New(worker)
	if err != nil {
		_ = blocker.Rollback(context.Background())
		_ = worker.Rollback(context.Background())
		t.Fatal(err)
	}
	orderInput := []domain.Person{
		{NSID: ordered[1].NSID, Username: ordered[1].Username, Realname: "after-b"},
		{NSID: ordered[0].NSID, Username: ordered[0].Username, Realname: "after-a"},
	}
	orderCtx, stopOrder := context.WithCancel(ctx)
	defer stopOrder()
	orderDone := make(chan error, 1)
	go func() { orderDone <- workerRepository.RefreshChangedPeople(orderCtx, orderInput) }()
	waitDeadline := time.NewTimer(2 * time.Second)
	waitTicker := time.NewTicker(10 * time.Millisecond)
	waiting := false
	for !waiting {
		select {
		case <-waitDeadline.C:
			_ = blocker.Rollback(context.Background())
			<-orderDone
			t.Fatal("refresh did not wait for the lowest NSID row lock")
		case <-waitTicker.C:
			if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND wait_event_type='Lock' AND wait_event='transactionid')`, applicationName).Scan(&waiting); err != nil {
				_ = blocker.Rollback(context.Background())
				<-orderDone
				t.Fatal(err)
			}
		}
	}
	waitDeadline.Stop()
	waitTicker.Stop()
	var highXmax int64
	if err := pool.QueryRow(ctx, `SELECT xmax::text::bigint FROM people WHERE nsid=$1`, ordered[1].NSID).Scan(&highXmax); err != nil {
		_ = blocker.Rollback(context.Background())
		<-orderDone
		t.Fatal(err)
	}
	if highXmax != 0 {
		_ = blocker.Rollback(context.Background())
		<-orderDone
		t.Fatalf("higher NSID row was locked before the blocked lower row: xmax=%d", highXmax)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-orderDone; err != nil {
		_ = worker.Rollback(context.Background())
		t.Fatalf("ordered refresh: %v", err)
	}
	if err := worker.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	repositoryA, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	repositoryB, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 200; iteration++ {
		left := []domain.Person{
			{NSID: people[2].NSID, Username: people[2].Username, Realname: fmt.Sprintf("left-%03d-c", iteration)},
			{NSID: people[1].NSID, Username: people[1].Username, Realname: fmt.Sprintf("left-%03d-b", iteration)},
		}
		right := []domain.Person{
			{NSID: people[1].NSID, Username: people[1].Username, Realname: fmt.Sprintf("right-%03d-b", iteration)},
			{NSID: people[2].NSID, Username: people[2].Username, Realname: fmt.Sprintf("right-%03d-c", iteration)},
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			results <- repositoryA.RefreshChangedPeople(ctx, left)
		}()
		go func() {
			defer wait.Done()
			<-start
			results <- repositoryB.RefreshChangedPeople(ctx, right)
		}()
		close(start)
		wait.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("opposite-order iteration %d: %v", iteration, err)
			}
		}
	}
}

func TestRefreshChangedPeopleAvoidsForeignKeyLockDeadlock(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("refresh-fk-lock-%d", time.Now().UnixNano())
	people := []domain.Person{
		{NSID: prefix + "-a", Username: prefix + "-a", Realname: "before-a"},
		{NSID: prefix + "-b", Username: prefix + "-b", Realname: "before-b"},
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM people WHERE nsid LIKE $1`, prefix+"%"); err != nil {
			t.Errorf("delete test people: %v", err)
		}
	})
	photoIDs := []int64{time.Now().UnixNano(), time.Now().UnixNano() + 1}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1::bigint[])`, photoIDs); err != nil {
			t.Errorf("delete test photos: %v", err)
		}
	})
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
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = txB.Rollback(context.Background()) })
	for i, tx := range []pgx.Tx{txA, txB} {
		if _, err := tx.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,raw,content_hash)
			VALUES($1,$2,now(),'public',$3,$4)`, photoIDs[i], people[i].NSID, []byte("{}"), make([]byte, 32)); err != nil {
			t.Fatalf("insert photo foreign key %d: %v", i, err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		txRepository, repoErr := postgres.New(txA)
		if repoErr == nil {
			repoErr = txRepository.RefreshChangedPeople(ctx, []domain.Person{{NSID: people[1].NSID, Username: people[1].Username, Realname: "after-a"}})
		}
		results <- repoErr
	}()
	go func() {
		<-start
		txRepository, repoErr := postgres.New(txB)
		if repoErr == nil {
			repoErr = txRepository.RefreshChangedPeople(ctx, []domain.Person{{NSID: people[0].NSID, Username: people[0].Username, Realname: "after-b"}})
		}
		results <- repoErr
	}()
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("refresh overlapping page after foreign-key write: %v", err)
		}
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("commit transaction A: %v", err)
	}
	if err := txB.Commit(ctx); err != nil {
		t.Fatalf("commit transaction B: %v", err)
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
	var profileMatches, facetStateMatches bool
	if err := pool.QueryRow(ctx, `SELECT username, realname, profile = $2::jsonb, icon, photos_count, facet_state = $3::jsonb FROM people WHERE nsid=$1`,
		nsid, original.Profile, original.FacetState).
		Scan(&got.Username, &got.Realname, &profileMatches, &got.Icon, &got.PhotosCount, &facetStateMatches); err != nil {
		t.Fatal(err)
	}
	if got.Username != original.Username || got.Realname != original.Realname || !profileMatches || got.Icon != original.Icon || got.PhotosCount != original.PhotosCount || !facetStateMatches {
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

func TestSyntheticPeopleContention(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" || os.Getenv("XFLICKR_PEOPLE_CONTENTION") != "1" {
		t.Skip("run synthetic-people-upserts.sh to measure 8 overlapping writers for 30s")
	}
	const workers = 8
	const peopleCount = 500
	const duration = 30 * time.Second
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	prefix := fmt.Sprintf("people-contention-%d", time.Now().UnixNano())
	cleanupPeople(t, pool, prefix)
	people := make([]domain.Person, peopleCount)
	for i := range people {
		nsid := fmt.Sprintf("%s-%03d", prefix, i)
		people[i] = domain.Person{NSID: nsid, Username: nsid}
	}
	seed, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.UpsertPeople(ctx, people); err != nil {
		t.Fatalf("seed overlapping people: %v", err)
	}
	applicationName := fmt.Sprintf("xflickr_people_contention_%d", time.Now().UnixNano())
	repositories := make([]*postgres.Repository, workers)
	connections := make([]*pgxpool.Conn, workers)
	for i := range repositories {
		connection, acquireErr := pool.Acquire(ctx)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		connections[i] = connection
		t.Cleanup(func() { connection.Release() })
		if _, err := connection.Exec(ctx, `SELECT set_config('application_name',$1,false)`, applicationName); err != nil {
			t.Fatal(err)
		}
		repositories[i], err = postgres.New(connection)
		if err != nil {
			t.Fatal(err)
		}
	}
	var batches atomic.Int64
	var batchNanos atomic.Int64
	var maxBatchNanos atomic.Int64
	var waitSamples atomic.Int64
	var waitingSessions atomic.Int64
	startGate := make(chan struct{})
	stopSampling := make(chan struct{})
	samplingDone := make(chan struct{})
	var workersDone sync.WaitGroup
	workerErrors := make(chan error, workers)
	workersDone.Add(workers)
	for _, repository := range repositories {
		go func(repository *postgres.Repository) {
			defer workersDone.Done()
			<-startGate
			for ctx.Err() == nil {
				started := time.Now()
				if err := repository.UpsertPeople(ctx, people); err != nil {
					if ctx.Err() == nil {
						workerErrors <- err
					}
					return
				}
				elapsed := time.Since(started).Nanoseconds()
				batches.Add(1)
				batchNanos.Add(elapsed)
				for previous := maxBatchNanos.Load(); elapsed > previous && !maxBatchNanos.CompareAndSwap(previous, elapsed); previous = maxBatchNanos.Load() {
				}
			}
		}(repository)
	}
	go func() {
		defer close(samplingDone)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSampling:
				return
			case <-ticker.C:
				var waiting int64
				if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND wait_event_type='Lock' AND wait_event='transactionid'`, applicationName).Scan(&waiting); err != nil {
					t.Errorf("sample transactionid waits: %v", err)
					return
				}
				if waiting > 0 {
					waitSamples.Add(1)
					waitingSessions.Add(waiting)
				}
			}
		}
	}()
	deadlocksBefore := databaseDeadlocks(t, pool)
	started := time.Now()
	close(startGate)
	timeout := time.NewTimer(duration)
	<-timeout.C
	cancel()
	workersDone.Wait()
	close(stopSampling)
	<-samplingDone
	elapsed := time.Since(started)
	deadlocksAfter := databaseDeadlocks(t, pool)
	close(workerErrors)
	for err := range workerErrors {
		t.Errorf("synthetic upsert: %v", err)
	}
	count := batches.Load()
	if count == 0 {
		t.Fatal("synthetic writers completed no batches")
	}
	averageBatch := time.Duration(batchNanos.Load() / count)
	fmt.Printf("people_contention workers=%d people_per_batch=%d elapsed=%s batches=%d batches_per_second=%.2f avg_batch=%s max_batch=%s transactionid_wait_samples=%d waiting_sessions_sampled=%d deadlocks_before=%d deadlocks_after=%d deadlocks_delta=%d\n",
		workers, peopleCount, elapsed.Round(time.Millisecond), count, float64(count)/elapsed.Seconds(), averageBatch.Round(time.Microsecond), time.Duration(maxBatchNanos.Load()).Round(time.Microsecond), waitSamples.Load(), waitingSessions.Load(), deadlocksBefore, deadlocksAfter, deadlocksAfter-deadlocksBefore)
}

func databaseDeadlocks(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var deadlocks int64
	if err := pool.QueryRow(t.Context(), `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&deadlocks); err != nil {
		t.Fatal(err)
	}
	return deadlocks
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
