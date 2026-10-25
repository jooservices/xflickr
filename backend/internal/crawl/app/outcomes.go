package app

import (
	"errors"
	"time"

	"github.com/jooservices/xflickr/pkg/flickr"
)

// Outcome is what the engine does with a failed call.
type Outcome int

const (
	// OutcomeRetry backs off and consumes an attempt.
	OutcomeRetry Outcome = iota
	// OutcomeWaitQuota holds the item without consuming an attempt.
	OutcomeWaitQuota
	// OutcomeDone finishes the item: the subject no longer exists.
	OutcomeDone
	// OutcomeStop stops the owner's items and notifies; the credential is bad.
	OutcomeStop
	// OutcomeRequeueBlocked reports the node as blocked and requeues the item.
	OutcomeRequeueBlocked
)

// Classify maps a Flickr error to the outcome the engine applies.
func Classify(err error) Outcome {
	switch {
	case errors.Is(err, flickr.ErrQuotaExceeded):
		return OutcomeWaitQuota
	case errors.Is(err, flickr.ErrNotFound):
		return OutcomeDone
	case errors.Is(err, flickr.ErrTokenRevoked), errors.Is(err, flickr.ErrInvalidKey):
		return OutcomeStop
	case errors.Is(err, flickr.ErrIPBlocked):
		return OutcomeRequeueBlocked
	default:
		return OutcomeRetry
	}
}

// Timing configures how long deferred items wait.
type Timing struct {
	MaxAttempts int
	Backoff     time.Duration
	QuotaWait   time.Duration
	BlockedWait time.Duration
}

// DefaultTiming is used for zero fields.
var DefaultTiming = Timing{MaxAttempts: 5, Backoff: 30 * time.Second, QuotaWait: time.Minute, BlockedWait: 5 * time.Minute}

func (t Timing) withDefaults() Timing {
	if t.MaxAttempts <= 0 {
		t.MaxAttempts = DefaultTiming.MaxAttempts
	}
	if t.Backoff <= 0 {
		t.Backoff = DefaultTiming.Backoff
	}
	if t.QuotaWait <= 0 {
		t.QuotaWait = DefaultTiming.QuotaWait
	}
	if t.BlockedWait <= 0 {
		t.BlockedWait = DefaultTiming.BlockedWait
	}
	return t
}

// retryDelay grows exponentially with the attempts already consumed.
func (t Timing) retryDelay(attempts int) time.Duration {
	delay := t.Backoff
	for i := 0; i < attempts && i < 10; i++ {
		delay *= 2
	}
	return delay
}

// StopReason says why a user's or key's items were stopped.
type StopReason string

const (
	StopTokenRevoked StopReason = "token_revoked"
	StopInvalidKey   StopReason = "invalid_key"
)

func stopReason(err error) StopReason {
	if errors.Is(err, flickr.ErrInvalidKey) {
		return StopInvalidKey
	}
	return StopTokenRevoked
}
