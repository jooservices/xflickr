package roles

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestConfiguredCheckpointRepairRejectsBadDatabase(t *testing.T) {
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_NAME", "xflickr_wtjooxf345")
	t.Setenv("DB_USER", "unused")
	t.Setenv("DB_PASSWORD", "unused")
	t.Setenv("DB_PORT", "invalid")
	if _, err := RepairZeroCheckpoints(t.Context(), CheckpointRepairOptions{BatchSize: 10}); err == nil {
		t.Fatal("invalid database accepted")
	}
	t.Setenv("DB_PORT", "1")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := RepairZeroCheckpoints(ctx, CheckpointRepairOptions{BatchSize: 10}); err == nil {
		t.Fatal("unreachable database accepted")
	}
}

func TestCheckpointRepairJobEntryRunsBatchLoopAndRepeatableEmptySecondRun(t *testing.T) {
	setCheckpointRepairDatabaseEnv(t)
	withIdleRolePool(t)

	var calls int
	var seenSizes []int
	previous := repairZeroCheckpoints
	repairZeroCheckpoints = func(_ context.Context, pool *pgxpool.Pool, options CheckpointRepairOptions) (CheckpointRepairReport, error) {
		if pool == nil {
			t.Fatal("repair ran without a pool")
		}
		calls++
		seenSizes = append(seenSizes, options.BatchSize)
		switch calls {
		case 1:
			// First CLI/job entry: one photos batch, one favorites batch, then
			// two empty closing batches — the same shape as the store loop.
			return CheckpointRepairReport{
				PhotosUpdated:    4,
				FavoritesUpdated: 2,
				Batches:          4,
				RunAt:            time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC),
			}, nil
		case 2:
			return CheckpointRepairReport{
				PhotosUpdated:    0,
				FavoritesUpdated: 0,
				Batches:          2,
				RunAt:            time.Date(2026, 11, 9, 8, 1, 0, 0, time.UTC),
			}, nil
		default:
			t.Fatalf("unexpected repair call %d", calls)
			return CheckpointRepairReport{}, nil
		}
	}
	t.Cleanup(func() { repairZeroCheckpoints = previous })

	first, err := RepairZeroCheckpoints(t.Context(), CheckpointRepairOptions{BatchSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if first.PhotosUpdated != 4 || first.FavoritesUpdated != 2 || first.Batches != 4 {
		t.Fatalf("first report=%+v", first)
	}

	second, err := RepairZeroCheckpoints(t.Context(), CheckpointRepairOptions{BatchSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if second.PhotosUpdated != 0 || second.FavoritesUpdated != 0 || second.Batches != 2 {
		t.Fatalf("second report=%+v", second)
	}
	if calls != 2 || len(seenSizes) != 2 || seenSizes[0] != 50 || seenSizes[1] != 50 {
		t.Fatalf("calls=%d sizes=%v", calls, seenSizes)
	}
}

func TestCheckpointRepairNothingToRepairReportsEmptyBatches(t *testing.T) {
	setCheckpointRepairDatabaseEnv(t)
	withIdleRolePool(t)

	previous := repairZeroCheckpoints
	repairZeroCheckpoints = func(_ context.Context, _ *pgxpool.Pool, options CheckpointRepairOptions) (CheckpointRepairReport, error) {
		if options.BatchSize != 100 {
			t.Fatalf("batch size=%d", options.BatchSize)
		}
		return CheckpointRepairReport{Batches: 2, RunAt: time.Unix(1, 0).UTC()}, nil
	}
	t.Cleanup(func() { repairZeroCheckpoints = previous })

	report, err := RepairZeroCheckpoints(t.Context(), CheckpointRepairOptions{BatchSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if report.PhotosUpdated != 0 || report.FavoritesUpdated != 0 || report.Batches != 2 {
		t.Fatalf("empty repair report=%+v", report)
	}
}

func TestCheckpointRepairPropagatesStoreError(t *testing.T) {
	setCheckpointRepairDatabaseEnv(t)
	withIdleRolePool(t)

	storeErr := errors.New("crawl: begin photo checkpoint repair: connection reset")
	previous := repairZeroCheckpoints
	repairZeroCheckpoints = func(context.Context, *pgxpool.Pool, CheckpointRepairOptions) (CheckpointRepairReport, error) {
		return CheckpointRepairReport{Batches: 1}, storeErr
	}
	t.Cleanup(func() { repairZeroCheckpoints = previous })

	report, err := RepairZeroCheckpoints(t.Context(), CheckpointRepairOptions{BatchSize: 10})
	if !errors.Is(err, storeErr) {
		t.Fatalf("err=%v", err)
	}
	if report.Batches != 1 {
		t.Fatalf("partial report=%+v", report)
	}
}

func TestCheckpointRepairHonoursCancelledContext(t *testing.T) {
	setCheckpointRepairDatabaseEnv(t)
	withIdleRolePool(t)

	previous := repairZeroCheckpoints
	repairZeroCheckpoints = func(ctx context.Context, _ *pgxpool.Pool, _ CheckpointRepairOptions) (CheckpointRepairReport, error) {
		if err := ctx.Err(); err != nil {
			return CheckpointRepairReport{}, err
		}
		t.Fatal("repair ran after cancellation")
		return CheckpointRepairReport{}, nil
	}
	t.Cleanup(func() { repairZeroCheckpoints = previous })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RepairZeroCheckpoints(ctx, CheckpointRepairOptions{BatchSize: 10}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func setCheckpointRepairDatabaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_NAME", "xflickr_wtjooxf345")
	t.Setenv("DB_USER", "unused")
	t.Setenv("DB_PASSWORD", "unused")
}

func withIdleRolePool(t *testing.T) {
	t.Helper()
	previous := openRoleDatabase
	openRoleDatabase = func(ctx context.Context, _ config.DBConfig) (*pgxpool.Pool, error) {
		cfg, err := pgxpool.ParseConfig("postgres://unused:unused@127.0.0.1:1/xflickr_wtjooxf345?sslmode=disable&connect_timeout=1")
		if err != nil {
			return nil, err
		}
		cfg.MinConns = 0
		cfg.MaxConns = 1
		return pgxpool.NewWithConfig(ctx, cfg)
	}
	t.Cleanup(func() { openRoleDatabase = previous })
}
