package postgres_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

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
