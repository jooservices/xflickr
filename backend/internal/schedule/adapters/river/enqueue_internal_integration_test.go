package river

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/app"
)

func TestPumpReturnsBacklogAndInflightReadErrors(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	settings := app.PumpSettings{PerKeyReadyCap: 1, NodeReadyCap: 1, MediaReadyCap: 1, AIReadyCap: 1, SyncReadyCap: 1, RetryMax: 1, LeaseTimeout: time.Second}
	store := Store{Pool: pool, listKeys: func(context.Context, pgx.Tx) ([]int64, error) { return nil, errors.New("backlog read") }}
	if _, err := store.Pump(ctx, settings, internalCapacity(1), internalQueue{}); err == nil {
		t.Fatal("Pump ignored pending key query error")
	}
	store = Store{Pool: pool, listKeys: func(context.Context, pgx.Tx) ([]int64, error) { return []int64{1}, nil }, inflight: func(context.Context, pgx.Tx, int64, string) (int, error) { return 0, errors.New("inflight read") }}
	if _, err := store.Pump(ctx, settings, internalCapacity(1), internalQueue{}); err == nil {
		t.Fatal("Pump ignored inflight query error")
	}
}

type internalCapacity int

func (c internalCapacity) Capacity(context.Context, int64, time.Duration) (int, error) {
	return int(c), nil
}

type internalQueue struct{}

func (internalQueue) Enqueue(context.Context, pgx.Tx, queue.Job, queue.Options) error { return nil }
func (internalQueue) Register(string, queue.Handler) error                            { return nil }

func internalIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}
