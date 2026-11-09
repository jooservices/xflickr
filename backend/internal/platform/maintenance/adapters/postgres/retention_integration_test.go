package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	maintenancepg "github.com/jooservices/xflickr/internal/platform/maintenance/adapters/postgres"
	maintenanceapp "github.com/jooservices/xflickr/internal/platform/maintenance/app"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
)

func TestDetachAndDropOldPartitionsKeepsCurrentAndFuture(t *testing.T) {
	pool, ctx := openRetentionDB(t)
	today := utcToday()
	oldDay := today.AddDate(0, 0, -40)
	future := today.AddDate(0, 0, 3)

	creator := quotaPostgres.Partitions{Pool: pool}
	if err := creator.MaintainCallLogPartitions(ctx, oldDay, future); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_log (key_id, class, called_at, method, status)
VALUES (NULL, NULL, $1, 'flickr.old', 'ok')`, oldDay.Add(6*time.Hour)); err != nil {
		t.Fatal(err)
	}

	job := maintenanceapp.Retention{
		Store:    maintenancepg.NewStore(pool),
		Settings: fixedSettings{days: 30},
		Now:      func() time.Time { return today.Add(12 * time.Hour) },
	}
	if err := job.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	assertRegclass(t, ctx, pool, "api_call_log_"+oldDay.Format("20060102"), false)
	assertRegclass(t, ctx, pool, "api_call_log_"+today.Format("20060102"), true)
	assertRegclass(t, ctx, pool, "api_call_log_"+future.Format("20060102"), true)
}

func TestRetentionRunTwiceIsIdempotent(t *testing.T) {
	pool, ctx := openRetentionDB(t)
	today := utcToday()
	oldDay := today.AddDate(0, 0, -45)
	if err := (quotaPostgres.Partitions{Pool: pool}).MaintainCallLogPartitions(ctx, oldDay, today.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	job := maintenanceapp.Retention{
		Store:    maintenancepg.NewStore(pool),
		Settings: fixedSettings{days: 30},
		Now:      func() time.Time { return today.Add(3 * time.Hour) },
	}
	if err := job.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := job.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertRegclass(t, ctx, pool, "api_call_log_"+oldDay.Format("20060102"), false)
	assertRegclass(t, ctx, pool, "api_call_log_"+today.Format("20060102"), true)
}

func TestInsertsStillWorkWhileDetaching(t *testing.T) {
	pool, ctx := openRetentionDB(t)
	today := utcToday()
	oldDay := today.AddDate(0, 0, -50)
	if err := (quotaPostgres.Partitions{Pool: pool}).MaintainCallLogPartitions(ctx, oldDay, today.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}

	var inserts atomic.Int64
	var insertErr atomic.Value
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, err := pool.Exec(context.Background(), `INSERT INTO api_call_log (key_id, class, called_at, method, status)
VALUES (NULL, NULL, $1, 'flickr.live', 'ok')`, time.Now().UTC())
			if err != nil {
				insertErr.Store(err.Error())
				return
			}
			inserts.Add(1)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	job := maintenanceapp.Retention{
		Store:    maintenancepg.NewStore(pool),
		Settings: fixedSettings{days: 30},
		Now:      func() time.Time { return today.Add(4 * time.Hour) },
	}
	if err := job.RunOnce(ctx); err != nil {
		close(stop)
		wg.Wait()
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
	if raw := insertErr.Load(); raw != nil {
		t.Fatalf("writer failed during detach: %v", raw)
	}
	if inserts.Load() == 0 {
		t.Fatal("writer made no inserts while retention ran")
	}
	assertRegclass(t, ctx, pool, "api_call_log_"+oldDay.Format("20060102"), false)
}

type fixedSettings struct {
	days   int
	dryRun bool
}

func (s fixedSettings) RetentionDays(context.Context) (int, error) { return s.days, nil }
func (s fixedSettings) DryRun(context.Context) (bool, error)       { return s.dryRun, nil }

func openRetentionDB(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg), ctx
}

func utcToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func assertRegclass(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, want bool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatalf("regclass %s: %v", name, err)
	}
	if exists != want {
		t.Fatalf("%s exists=%t, want %t", name, exists, want)
	}
}
