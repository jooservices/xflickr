package app

import (
	"context"
	"fmt"
	"testing"
)

type reclaimChangeCheckSpy struct{ calls int }

func (s *reclaimChangeCheckSpy) RunOnce(context.Context) error {
	s.calls++
	return nil
}

func TestPumpRejectsInvalidReclaimBatchSize(t *testing.T) {
	for _, source := range []bool{false, true} {
		for _, value := range []int{-1, 0, 10001} {
			t.Run(fmt.Sprintf("source=%t/batch=%d", source, value), func(t *testing.T) {
				pump, store, checks, lane := reclaimLimitPump(value, source)
				got, err := pump.RunOnce(t.Context())
				if err == nil || got != 0 {
					t.Fatalf("RunOnce() = %d, %v; want zero and error", got, err)
				}
				if store.reclaims != 0 || store.pumps != 0 || checks.calls != 0 || lane.limit != -1 {
					t.Fatalf("invalid tick had side effects: store=%+v checks=%d lane=%d", store, checks.calls, lane.limit)
				}
			})
		}
	}
}

func TestPumpPassesValidReclaimBatchSize(t *testing.T) {
	for _, source := range []bool{false, true} {
		for _, value := range []int{1, 100, 10000} {
			t.Run(fmt.Sprintf("source=%t/batch=%d", source, value), func(t *testing.T) {
				pump, store, checks, lane := reclaimLimitPump(value, source)
				if _, err := pump.RunOnce(t.Context()); err != nil {
					t.Fatal(err)
				}
				if store.reclaimBatchSize != value || store.reclaims != 1 || store.pumps != 1 || checks.calls != 1 || lane.limit != pump.Settings.SyncReadyCap {
					t.Fatalf("valid tick: store=%+v checks=%d lane=%d", store, checks.calls, lane.limit)
				}
			})
		}
	}
}

func reclaimLimitPump(value int, source bool) (Pump, *pumpFakeStore, *reclaimChangeCheckSpy, *photoLaneFake) {
	store := &pumpFakeStore{}
	checks := &reclaimChangeCheckSpy{}
	lane := &photoLaneFake{limit: -1}
	pump := Pump{Store: store, Credits: fakeCredits{}, Queue: fakeQueue{}, Settings: validPumpSettings(), ChangeChecks: checks, PhotoLane: lane}
	settings := validPumpSettings()
	settings.ReclaimBatchSize = value
	if source {
		pump.SettingsSource = &mutablePumpSettings{value: settings}
	} else {
		pump.Settings = settings
	}
	return pump, store, checks, lane
}
