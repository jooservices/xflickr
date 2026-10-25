package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/queue"
)

func TestCreditsBoundsAllCapacitySources(t *testing.T) {
	tests := []struct {
		name    string
		input   CreditInput
		want    int
		wantErr bool
	}{
		{"quota", CreditInput{RemainingQuota: 7, PerKeyReadyCap: 20, NodeCap: 30, WorkerCap: 40}, 7, false},
		{"key cap", CreditInput{RemainingQuota: 30, PerKeyReadyCap: 12, NodeCap: 30, WorkerCap: 40}, 12, false},
		{"node free", CreditInput{RemainingQuota: 30, PerKeyReadyCap: 30, NodeCap: 15, Inflight: 9, WorkerCap: 40}, 6, false},
		{"worker free", CreditInput{RemainingQuota: 30, PerKeyReadyCap: 30, NodeCap: 40, WorkerCap: 20, WorkerInflight: 18}, 2, false},
		{"saturated", CreditInput{NodeCap: 1, Inflight: 3}, 0, false},
		{"negative", CreditInput{RemainingQuota: -1}, 0, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Credits(test.input)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("Credits() = %d, %v; want %d, err=%v", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestPumpRunOnceValidationAndDependencies(t *testing.T) {
	if _, err := (Pump{}).RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce accepted missing dependencies")
	}
	pump := Pump{Store: &pumpFakeStore{}, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings()}
	if _, err := pump.RunOnce(nil); err == nil {
		t.Fatal("RunOnce accepted nil context")
	}
	pump.Settings.RetryMax = -1
	if _, err := pump.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce accepted invalid settings")
	}
	pump.Settings = validPumpSettings()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pump.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce canceled error = %v", err)
	}
}

func TestPumpRunOnceReclaimsAndDispatches(t *testing.T) {
	store := &pumpFakeStore{}
	pump := Pump{Store: store, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings()}
	got, err := pump.RunOnce(context.Background())
	if err != nil || got != 3 || store.reclaims != 1 || store.pumps != 1 {
		t.Fatalf("RunOnce() = %d, %v; store = %+v", got, err, store)
	}
	store.reclaimErr = errors.New("reclaim")
	if _, err := pump.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce ignored reclaim error")
	}
	store.reclaimErr = nil
	store.pumpErr = errors.New("pump")
	if _, err := pump.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce ignored pump error")
	}
}

func TestReclaimValidationAndClock(t *testing.T) {
	if _, err := (Reclaim{}).RunOnce(context.Background()); err == nil {
		t.Fatal("Reclaim accepted missing dependencies")
	}
	store := &pumpFakeStore{}
	reclaim := Reclaim{Store: store, RetryMax: 5, Backoff: 30 * time.Second, Now: func() time.Time { return time.Unix(42, 0) }}
	if _, err := reclaim.RunOnce(nil); err == nil {
		t.Fatal("Reclaim accepted nil context")
	}
	if _, err := reclaim.RunOnce(context.Background()); err != nil || !store.reclaimTime.Equal(time.Unix(42, 0)) {
		t.Fatalf("Reclaim() = %v, %v; time=%v", store.reclaims, err, store.reclaimTime)
	}
	reclaim.RetryMax = -1
	if _, err := reclaim.RunOnce(context.Background()); err == nil {
		t.Fatal("Reclaim accepted zero retry max")
	}
}

func TestPumpRunStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pump := Pump{}
	if err := pump.Run(ctx); err != nil {
		t.Fatalf("Run() = %v", err)
	}
}

func validPumpSettings() PumpSettings {
	return PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 10, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
}

type fakeCredits struct{}

func (fakeCredits) Capacity(context.Context, int64, time.Duration) (int, error) { return 10, nil }

type fakeQueue struct{}

func (fakeQueue) Enqueue(context.Context, pgx.Tx, queue.Job, queue.Options) error { return nil }
func (fakeQueue) Register(string, queue.Handler) error                            { return nil }

type pumpFakeStore struct {
	pumps, reclaims     int
	pumpErr, reclaimErr error
	reclaimTime         time.Time
}

func (s *pumpFakeStore) Pump(context.Context, PumpSettings, CreditProvider, queue.JobQueue) (int, error) {
	s.pumps++
	return 3, s.pumpErr
}

func (s *pumpFakeStore) Reclaim(_ context.Context, _ int, _ time.Duration, now time.Time) (int, error) {
	s.reclaims++
	s.reclaimTime = now
	return 0, s.reclaimErr
}
