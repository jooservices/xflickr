package river

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/rivertype"

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

func TestRescorePersonValidatesDependencies(t *testing.T) {
	if _, err := (Store{}).RescorePerson(context.Background(), 1, "p", domain.ScoreWeights{}, 1); err == nil {
		t.Fatal("RescorePerson accepted nil pool")
	}
	pool := closedPool(t)
	if _, err := (Store{Pool: pool}).RescorePerson(context.Background(), 0, "p", domain.ScoreWeights{}, 1); err == nil {
		t.Fatal("RescorePerson accepted invalid user")
	}
	if err := (PersonRescorer{}).RescorePerson(context.Background(), 1, "p"); err == nil {
		t.Fatal("PersonRescorer accepted nil pool")
	}
	if err := (PersonRescorer{Pool: pool}).RescorePerson(context.Background(), 1, "p"); err == nil {
		t.Fatal("PersonRescorer ignored settings failure")
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

// A lease must not expire while the job waits in the River queue, and a live
// job must never be reaped: every non-final River state counts as live.
func TestLiveRiverStatesCoverEveryNonFinalState(t *testing.T) {
	for _, state := range rivertype.JobStates() {
		final := state == rivertype.JobStateCancelled || state == rivertype.JobStateCompleted || state == rivertype.JobStateDiscarded
		if got := strings.Contains(liveRiverStates, "'"+string(state)+"'"); got == final {
			t.Errorf("state %q: live=%v, want %v", state, got, !final)
		}
	}
}
