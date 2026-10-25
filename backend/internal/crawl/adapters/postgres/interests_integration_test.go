package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestAddIsIdempotentAndKeepsLowestDepthPerUser(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load worktree database configuration: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, dbConfig)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	person := fmt.Sprintf("interest-integration-%d", time.Now().UnixNano())
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Add(ctx, domain.Interest{UserID: userID, PersonNSID: person, Depth: 3, Via: "favorite"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Add(ctx, domain.Interest{UserID: userID, PersonNSID: person, Depth: 1, Via: "contact"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Add(ctx, domain.Interest{UserID: userID, PersonNSID: person, Depth: 2, Via: "other"}); err != nil {
		t.Fatal(err)
	}
	var count int
	var depth int
	var via string
	if err := pool.QueryRow(ctx, `SELECT count(*), min(depth), min(via) FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2 GROUP BY user_id,person_nsid`, userID, person).Scan(&count, &depth, &via); err != nil {
		t.Fatalf("read stored interest: %v", err)
	}
	if count != 1 || depth != 1 || via != "contact" {
		t.Fatalf("stored interest = count %d depth %d via %q, want one depth 1 via contact", count, depth, via)
	}
}

func insertInterestTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email,password_hash,role) VALUES ($1,'integration-hash','user') RETURNING id`, fmt.Sprintf("crawl-interest-%d@example.test", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
	return userID
}
