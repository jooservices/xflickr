package app

import (
	"errors"
	"time"

	"github.com/jooservices/xflickr/pkg/flickr"
)

// CredentialDecryptionReason is a stable, non-sensitive marker stored with
// deferred work so operational health can identify a keyring problem.
const CredentialDecryptionReason = "credential_decryption_unavailable" //nolint:gosec // Stable diagnostic marker, not secret material.

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
	// OutcomeUnavailable ends the item as failed with a stored reason: Flickr
	// denied access to this one resource, so retrying cannot help.
	OutcomeUnavailable
	// OutcomeRequeueBlocked reports the node as blocked and requeues the item.
	OutcomeRequeueBlocked
)

// Classify maps a Flickr error to the outcome the engine applies.
func Classify(err error) Outcome {
	switch {
	case errors.Is(err, flickr.ErrQuotaExceeded):
		return OutcomeWaitQuota
	case errors.Is(err, flickr.ErrTokenRevoked), errors.Is(err, flickr.ErrInvalidKey):
		return OutcomeStop
	case errors.Is(err, flickr.ErrResourceDenied):
		return OutcomeUnavailable
	case errors.Is(err, flickr.ErrNotFound):
		return OutcomeDone
	case errors.Is(err, flickr.ErrIPBlocked):
		return OutcomeRequeueBlocked
	default:
		return OutcomeRetry
	}
}

func isRecoverableFlickrFailure(err error) bool {
	return errors.Is(err, flickr.ErrRateLimited) || errors.Is(err, flickr.ErrServiceUnavailable)
}

// UnavailableReason is the terminal reason stored for a resource Flickr denied.
func UnavailableReason(err error) string {
	return "unavailable: " + err.Error()
}

// MaxRetryDelay caps the backoff between attempts of a recoverable failure.
const MaxRetryDelay = 5 * time.Minute

// Timing configures how long deferred items wait.
type Timing struct {
	MaxAttempts int
	// RecoverableMaxAttempts bounds attempts for 429/105 style failures. It is
	// larger than MaxAttempts because the failure is Flickr's, not the item's.
	RecoverableMaxAttempts int
	Backoff                time.Duration
	QuotaWait              time.Duration
	BlockedWait            time.Duration
	CredentialWait         time.Duration
}

// DefaultTiming is used for zero fields.
var DefaultTiming = Timing{MaxAttempts: 5, RecoverableMaxAttempts: 20, Backoff: 30 * time.Second, QuotaWait: time.Minute, BlockedWait: 5 * time.Minute, CredentialWait: 15 * time.Minute}

func (t Timing) withDefaults() Timing {
	if t.MaxAttempts <= 0 {
		t.MaxAttempts = DefaultTiming.MaxAttempts
	}
	if t.RecoverableMaxAttempts <= 0 {
		t.RecoverableMaxAttempts = DefaultTiming.RecoverableMaxAttempts
	}
	if t.RecoverableMaxAttempts < t.MaxAttempts {
		t.RecoverableMaxAttempts = t.MaxAttempts
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
	if t.CredentialWait <= 0 {
		t.CredentialWait = DefaultTiming.CredentialWait
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
