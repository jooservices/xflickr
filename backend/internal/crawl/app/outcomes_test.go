package app

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jooservices/xflickr/pkg/flickr"
)

func TestClassifyFlickrErrors(t *testing.T) {
	denied := &flickr.CallError{Kind: errors.Join(flickr.ErrResourceDenied, flickr.ErrPermission), Code: 98}
	cases := []struct {
		name string
		err  error
		want Outcome
	}{
		{name: "quota waits", err: flickr.ErrQuotaExceeded, want: OutcomeWaitQuota},
		{name: "rate limited retries", err: flickr.ErrRateLimited, want: OutcomeRetry},
		{name: "service unavailable retries", err: flickr.ErrServiceUnavailable, want: OutcomeRetry},
		{name: "resource denied is unavailable", err: fmt.Errorf("wrap: %w", denied), want: OutcomeUnavailable},
		{name: "plain permission retries", err: flickr.ErrPermission, want: OutcomeRetry},
		{name: "revoked token stops", err: flickr.ErrTokenRevoked, want: OutcomeStop},
		{name: "invalid key stops", err: flickr.ErrInvalidKey, want: OutcomeStop},
		{name: "not found is done", err: flickr.ErrNotFound, want: OutcomeDone},
		{name: "ip blocked requeues", err: flickr.ErrIPBlocked, want: OutcomeRequeueBlocked},
		{name: "unknown retries", err: errors.New("boom"), want: OutcomeRetry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Fatalf("Classify() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestIsRecoverableFlickrFailure(t *testing.T) {
	for err, want := range map[error]bool{
		flickr.ErrRateLimited:                      true,
		flickr.ErrServiceUnavailable:               true,
		fmt.Errorf("x: %w", flickr.ErrRateLimited): true,
		flickr.ErrQuotaExceeded:                    false,
		flickr.ErrTransient:                        false,
		flickr.ErrNotFound:                         false,
	} {
		if got := isRecoverableFlickrFailure(err); got != want {
			t.Errorf("isRecoverableFlickrFailure(%v) = %v, want %v", err, got, want)
		}
	}
}

func TestTimingDefaultsAndRetryDelay(t *testing.T) {
	timing := Timing{MaxAttempts: 8}.withDefaults()
	if timing.RecoverableMaxAttempts != DefaultTiming.RecoverableMaxAttempts || timing.Backoff != 30*time.Second {
		t.Fatalf("defaults = %+v", timing)
	}
	if got := (Timing{MaxAttempts: 30}).withDefaults().RecoverableMaxAttempts; got != 30 {
		t.Fatalf("recoverable budget %d must not be below MaxAttempts", got)
	}
	if got := timing.retryDelay(2); got != 2*time.Minute {
		t.Fatalf("retryDelay(2) = %s", got)
	}
}
