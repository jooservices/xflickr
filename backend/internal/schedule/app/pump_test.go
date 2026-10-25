package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/queue"
)

type changeCheckFake struct{ err error }

func (f changeCheckFake) RunOnce(context.Context) error { return f.err }

type photoLaneFake struct {
	limit int
	ids   []int64
	err   error
}

func (f *photoLaneFake) Next(_ context.Context, limit int) ([]int64, error) {
	f.limit = limit
	return f.ids, f.err
}

func TestCreditsBoundsAllCapacitySources(t *testing.T) {
	tests := []struct {
		name    string
		input   CreditInput
		want    int
		wantErr bool
	}{
		{"quota", CreditInput{RemainingQuota: 7, PerKeyReadyCap: 20, NodeCap: 30, WorkerCap: 40}, 7, false},
		{"key cap", CreditInput{RemainingQuota: 30, PerKeyReadyCap: 12, NodeCap: 30, WorkerCap: 40}, 12, false},
		{"key free", CreditInput{RemainingQuota: 30, PerKeyReadyCap: 15, KeyInflight: 9, NodeCap: 40, WorkerCap: 40}, 6, false},
		{"node free", CreditInput{RemainingQuota: 30, PerKeyReadyCap: 30, NodeCap: 15, NodeInflight: 9, WorkerCap: 40}, 6, false},
		{"worker free", CreditInput{RemainingQuota: 30, PerKeyReadyCap: 30, NodeCap: 40, WorkerCap: 20, WorkerInflight: 18}, 2, false},
		{"saturated", CreditInput{NodeCap: 1, NodeInflight: 3}, 0, false},
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
	pump.Settings.AgingBatchSize = 1
	pump.Settings.ScoreWeights.AgeMinutes = -1
	if _, err := pump.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce accepted invalid score settings")
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

func TestPumpDispatchesPhotoLaneWithinSyncCap(t *testing.T) {
	lane := &photoLaneFake{ids: []int64{41, 42}}
	pump := Pump{Store: &pumpFakeStore{}, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings(), PhotoLane: lane}
	got, err := pump.RunOnce(context.Background())
	if err != nil || got != 5 || lane.limit != 10 {
		t.Fatalf("RunOnce() = %d, %v; photo limit = %d", got, err, lane.limit)
	}
	pump.Settings.SyncReadyCap = 0
	lane.limit = -1
	if _, err := pump.RunOnce(context.Background()); err != nil || lane.limit != -1 {
		t.Fatalf("RunOnce() with zero sync cap = %v; photo limit = %d", err, lane.limit)
	}
	pump.Settings.SyncReadyCap = 10
	lane.err = errors.New("photo lane failed")
	if got, err := pump.RunOnce(context.Background()); got != 3 || !errors.Is(err, lane.err) {
		t.Fatalf("RunOnce() with photo failure = %d, %v", got, err)
	}
}

func TestPumpRunOnceSchedulesChecksBeforeDispatch(t *testing.T) {
	store := &pumpFakeStore{}
	pump := Pump{Store: store, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings(), ChangeChecks: changeCheckFake{}}
	if _, err := pump.RunOnce(context.Background()); err != nil || store.pumps != 1 {
		t.Fatalf("RunOnce() = %v, pumps=%d", err, store.pumps)
	}
	checkErr := errors.New("change check scheduling failed")
	pump.ChangeChecks = changeCheckFake{err: checkErr}
	var output bytes.Buffer
	pump.Logger = slog.New(slog.NewTextHandler(&output, nil))
	got, err := pump.RunOnce(context.Background())
	if err != nil || got != 3 || store.pumps != 2 || store.reclaims != 2 {
		t.Fatalf("RunOnce() after auxiliary error = %d, %v; pumps=%d reclaims=%d", got, err, store.pumps, store.reclaims)
	}
	if !strings.Contains(output.String(), "scheduler change check pass failed") || !strings.Contains(output.String(), checkErr.Error()) {
		t.Fatalf("RunOnce() log = %q, want change check failure", output.String())
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

func TestPumpRunLogsTickFailures(t *testing.T) {
	store := &pumpFakeStore{reclaimErr: errors.New("quota: governor offline")}
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	pump := Pump{Store: store, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings(), Logger: slog.New(slog.NewTextHandler(&output, nil))}
	if err := pump.Run(ctx); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.Contains(output.String(), "scheduler pump tick failed") || !strings.Contains(output.String(), "quota: governor offline") {
		t.Fatalf("Run() log = %q, want scheduler and governor failure", output.String())
	}
}

func TestPumpRunPublishesQueueStateImmediatelyAndOnInterval(t *testing.T) {
	publisher := &statePublisherStub{}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Millisecond)
	defer cancel()
	pump := Pump{Store: &pumpFakeStore{}, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings(), QueueState: publisher, QueueStateInterval: 5 * time.Millisecond}
	if err := pump.Run(ctx); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if publisher.calls < 2 {
		t.Fatalf("queue state publications = %d, want initial and periodic publication", publisher.calls)
	}
}

func TestPumpBackPressureDisablesMediaDispatch(t *testing.T) {
	store := &pumpFakeStore{}
	pump := Pump{Store: store, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings(), MediaPressure: pressureStub{full: true}}
	if _, err := pump.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.lastSettings.MediaReadyCap != 0 || store.lastSettings.AIReadyCap != validPumpSettings().AIReadyCap || store.lastSettings.SyncReadyCap != validPumpSettings().SyncReadyCap {
		t.Fatalf("pump settings under pressure = %+v", store.lastSettings)
	}
	var output bytes.Buffer
	pump.Logger = slog.New(slog.NewTextHandler(&output, nil))
	pump.MediaPressure = pressureStub{err: errors.New("storage check failed")}
	if _, err := pump.RunOnce(context.Background()); err != nil {
		t.Fatalf("pressure check failure stopped the pump: %v", err)
	}
	if store.lastSettings.MediaReadyCap != 0 || store.lastSettings.AIReadyCap != validPumpSettings().AIReadyCap || store.lastSettings.SyncReadyCap != validPumpSettings().SyncReadyCap {
		t.Fatalf("pump settings after pressure error = %+v", store.lastSettings)
	}
	if !strings.Contains(output.String(), "storage check failed") {
		t.Fatalf("pressure error not logged: %q", output.String())
	}
}

func TestPumpRunLogsQueueStatePublicationFailures(t *testing.T) {
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	pump := Pump{Store: &pumpFakeStore{}, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings(), QueueState: &statePublisherStub{err: errors.New("outbox offline")}, Logger: slog.New(slog.NewTextHandler(&output, nil))}
	if err := pump.Run(ctx); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.Contains(output.String(), "scheduler queue state publication failed") || !strings.Contains(output.String(), "outbox offline") {
		t.Fatalf("Run() log = %q, want queue state publication failure", output.String())
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
	lastSettings        PumpSettings
}

type statePublisherStub struct {
	calls int
	err   error
}

func (s *statePublisherStub) PublishQueueState(context.Context) error {
	s.calls++
	return s.err
}

func (s *pumpFakeStore) Pump(_ context.Context, settings PumpSettings, _ CreditProvider, _ queue.JobQueue) (int, error) {
	s.pumps++
	s.lastSettings = settings
	return 3, s.pumpErr
}

type pressureStub struct {
	full bool
	err  error
}

func (s pressureStub) Full(context.Context) (bool, error) { return s.full, s.err }

func (s *pumpFakeStore) Reclaim(_ context.Context, _ int, _ time.Duration, now time.Time) (int, error) {
	s.reclaims++
	s.reclaimTime = now
	return 0, s.reclaimErr
}
