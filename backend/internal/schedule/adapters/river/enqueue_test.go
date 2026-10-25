package river

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

func TestLoadSettingsValidatesDependenciesAndDatabaseErrors(t *testing.T) {
	if _, err := LoadSettings(context.Background(), nil); err == nil {
		t.Fatal("LoadSettings accepted nil pool")
	}
	pool := closedPool(t)
	if _, err := LoadSettings(nil, pool); err == nil {
		t.Fatal("LoadSettings accepted nil context")
	}
	if _, err := LoadSettings(context.Background(), pool); err == nil {
		t.Fatal("LoadSettings ignored database error")
	}
}

func TestRecomputeScoresValidatesDependencies(t *testing.T) {
	if _, err := (Store{}).RecomputeScores(context.Background(), 1, domain.ScoreWeights{}, 1); err == nil {
		t.Fatal("RecomputeScores accepted nil pool")
	}
	if _, err := (Store{Pool: closedPool(t)}).RecomputeScores(nil, 1, domain.ScoreWeights{}, 1); err == nil {
		t.Fatal("RecomputeScores accepted nil context")
	}
	if _, err := (Store{Pool: closedPool(t)}).RecomputeScores(context.Background(), 0, domain.ScoreWeights{}, 1); err == nil {
		t.Fatal("RecomputeScores accepted empty batch")
	}
}

func TestPumpValidatesDependenciesContextAndDatabase(t *testing.T) {
	settings := app.PumpSettings{PerKeyReadyCap: 1, NodeReadyCap: 1, MediaReadyCap: 1, AIReadyCap: 1, SyncReadyCap: 1, RetryMax: 1, LeaseTimeout: time.Second}
	if _, err := (Store{}).Pump(context.Background(), settings, nil, nil); err == nil {
		t.Fatal("Pump accepted missing dependencies")
	}
	if _, err := (Store{Pool: closedPool(t)}).Pump(context.Background(), settings, capacity(1), queueFake{}); err == nil {
		t.Fatal("Pump ignored acquire error")
	}
	if _, err := (Store{Pool: closedPool(t)}).Pump(nil, settings, capacity(1), queueFake{}); err == nil {
		t.Fatal("Pump accepted nil context")
	}
}

func TestReclaimValidatesPoolAndDatabase(t *testing.T) {
	if _, err := (Store{}).Reclaim(context.Background(), 5, 30*time.Second, time.Now()); err == nil {
		t.Fatal("Reclaim accepted nil pool")
	}
	if _, err := (Store{Pool: closedPool(t)}).Reclaim(context.Background(), 5, 30*time.Second, time.Now()); err == nil {
		t.Fatal("Reclaim ignored database error")
	}
}

type capacity int

func (c capacity) Capacity(context.Context, int64, time.Duration) (int, error) { return int(c), nil }

type queueFake struct{}

func (queueFake) Enqueue(context.Context, pgx.Tx, queue.Job, queue.Options) error {
	return errors.New("unused")
}
func (queueFake) Register(string, queue.Handler) error { return nil }

func closedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://xflickr:unused@127.0.0.1:1/xflickr_wtunit?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	return pool
}
