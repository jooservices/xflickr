// Package breaker provides a small concurrency-safe circuit breaker for
// external service calls.
package breaker

import (
	"errors"
	"sync"
	"time"
)

// ErrInvalidConfiguration reports a breaker configuration with a non-positive
// failure threshold or reset timeout.
var ErrInvalidConfiguration = errors.New("breaker: failure threshold and reset timeout must be positive")

// Config controls when a breaker opens and how long it stays open.
type Config struct {
	FailureThreshold int
	ResetTimeout     time.Duration
}

// Breaker counts consecutive failures and allows one probe after the reset
// timeout. A successful probe closes the breaker; a failed probe reopens it.
type Breaker struct {
	mu            sync.Mutex
	threshold     int
	resetTimeout  time.Duration
	failures      int
	openedAt      time.Time
	halfOpenProbe bool
}

// New creates a closed breaker with a positive failure threshold and timeout.
func New(config Config) (*Breaker, error) {
	if config.FailureThreshold < 1 || config.ResetTimeout <= 0 {
		return nil, ErrInvalidConfiguration
	}
	return &Breaker{threshold: config.FailureThreshold, resetTimeout: config.ResetTimeout}, nil
}

// Allow reports whether a call may proceed. Once the cooldown has elapsed,
// only one concurrent call is allowed to probe the service.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < b.threshold {
		return true
	}
	if time.Since(b.openedAt) < b.resetTimeout || b.halfOpenProbe {
		return false
	}
	b.halfOpenProbe = true
	return true
}

// RecordSuccess closes the breaker and clears its consecutive-failure count.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	b.failures = 0
	b.openedAt = time.Time{}
	b.halfOpenProbe = false
	b.mu.Unlock()
}

// RecordFailure advances the consecutive-failure count and opens or reopens
// the breaker when the threshold has been reached.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	if b.failures < b.threshold {
		b.failures++
	}
	if b.failures >= b.threshold {
		b.openedAt = time.Now()
		b.halfOpenProbe = false
	}
	b.mu.Unlock()
}

// RecordAbandoned releases an in-flight half-open probe without treating the
// result as a provider success or failure, such as when the caller cancels.
func (b *Breaker) RecordAbandoned() {
	b.mu.Lock()
	b.halfOpenProbe = false
	b.mu.Unlock()
}
