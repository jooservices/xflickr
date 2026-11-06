package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/rest"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestIdempotencyPostgresIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("requires shared worktree PostgreSQL")
	}
	_, source, _, _ := runtime.Caller(0)
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(filepath.Dir(source), "../../../../../../.env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	store := Store{DB: pool}
	ctx := context.Background()
	principal := fmt.Sprintf("rest-integration-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM rest_idempotency WHERE principal=$1 OR principal=$2", principal, principal+"other")
		if err != nil {
			t.Error(err)
		}
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	var calls atomic.Int32
	execute := func(context.Context) (rest.StoredResponse, error) {
		calls.Add(1)
		return rest.StoredResponse{Status: 202, Body: []byte("result")}, nil
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := rest.Idempotent(ctx, store, principal, "key", "fp", now, execute)
			if err != nil {
				var problem *rest.Problem
				if !errors.As(err, &problem) || problem.Status != 409 {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	result, err := rest.Idempotent(ctx, store, principal, "key", "fp", now, execute)
	if err != nil || result.Status != 202 || string(result.Body) != "result" || calls.Load() != 1 {
		t.Fatal(result, calls.Load(), err)
	}
	if _, err := rest.Idempotent(ctx, store, principal, "key", "changed", now, execute); err == nil {
		t.Fatal("conflicting body accepted")
	}
	if _, err := rest.Idempotent(ctx, store, principal+"other", "key", "fp", now, execute); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("principal scope", calls.Load())
	}
	// Expiry is checked by PostgreSQL, not by an untrusted client's clock.
	if _, err := pool.Exec(ctx, "UPDATE rest_idempotency SET expires_at=CURRENT_TIMESTAMP-interval '1 second' WHERE principal=$1", principal); err != nil {
		t.Fatal(err)
	}
	if _, err := rest.Idempotent(ctx, store, principal, "key", "new", now, execute); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatal("expired claim was not replaced")
	}
}
