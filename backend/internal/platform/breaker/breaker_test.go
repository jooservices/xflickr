package breaker

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{name: "valid", config: Config{FailureThreshold: 5, ResetTimeout: time.Minute}, valid: true},
		{name: "zero threshold", config: Config{ResetTimeout: time.Minute}},
		{name: "negative threshold", config: Config{FailureThreshold: -1, ResetTimeout: time.Minute}},
		{name: "zero reset timeout", config: Config{FailureThreshold: 1}},
		{name: "negative reset timeout", config: Config{FailureThreshold: 1, ResetTimeout: -time.Second}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			breaker, err := New(test.config)
			if test.valid {
				if err != nil || breaker == nil {
					t.Fatalf("New() = %v, %v; want non-nil breaker", breaker, err)
				}
				return
			}
			if breaker != nil || !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("New() = %v, %v; want ErrInvalidConfiguration", breaker, err)
			}
		})
	}
}

func TestFailureThresholdCooldownAndHalfOpenProbe(t *testing.T) {
	breaker, err := New(Config{FailureThreshold: 2, ResetTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !breaker.Allow() {
		t.Fatal("new breaker denied a call")
	}
	breaker.RecordFailure()
	if !breaker.Allow() {
		t.Fatal("breaker opened before the threshold")
	}
	breaker.RecordFailure()
	if breaker.Allow() {
		t.Fatal("breaker allowed a call before its cooldown elapsed")
	}

	time.Sleep(25 * time.Millisecond)
	if !breaker.Allow() {
		t.Fatal("breaker denied the first half-open probe after cooldown")
	}
	if breaker.Allow() {
		t.Fatal("breaker allowed more than one concurrent half-open probe")
	}
	breaker.RecordFailure()
	if breaker.Allow() {
		t.Fatal("failed half-open probe did not reopen the breaker")
	}
	time.Sleep(25 * time.Millisecond)
	if !breaker.Allow() {
		t.Fatal("breaker did not allow another probe after reopening cooldown")
	}
	breaker.RecordAbandoned()
	if !breaker.Allow() {
		t.Fatal("abandoned half-open probe was not released")
	}
	breaker.RecordSuccess()
	if !breaker.Allow() {
		t.Fatal("successful probe did not close the breaker")
	}
}

func TestSuccessResetsConsecutiveFailures(t *testing.T) {
	breaker, err := New(Config{FailureThreshold: 2, ResetTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	breaker.RecordFailure()
	breaker.RecordSuccess()
	breaker.RecordFailure()
	if !breaker.Allow() {
		t.Fatal("success did not reset the consecutive failure count")
	}
}

func TestConcurrentFailureUpdatesAreSafe(t *testing.T) {
	breaker, err := New(Config{FailureThreshold: 100, ResetTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 5 {
				breaker.Allow()
				breaker.RecordFailure()
			}
		}()
	}
	workers.Wait()
	if breaker.Allow() {
		t.Fatal("breaker remained closed after concurrent failures passed its threshold")
	}
}
