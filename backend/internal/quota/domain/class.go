// Package domain defines quota permits and scheduling classes.
package domain

import (
	"errors"
	"time"
)

// Class identifies the priority lane for a Flickr call.
type Class string

const (
	// Interactive calls are user-started and may use reserved hourly capacity.
	Interactive Class = "interactive"
	// Person calls belong to the contacts, photos, and related-record lane.
	Person Class = "person"
	// Photo calls belong to the photo-size retrieval lane.
	Photo Class = "photo"
)

var (
	// ErrInvalidClass means a class is not one of the supported quota lanes.
	ErrInvalidClass = errors.New("quota: invalid class")
	// ErrInvalidBudget means one or more per-key budget values are invalid.
	ErrInvalidBudget = errors.New("quota: invalid budget")
	// ErrGovernorOffline means Redis could not enforce quota, so the call is denied.
	ErrGovernorOffline = errors.New("quota: governor offline")
	// ErrLedgerNotReady means the rolling ledger has not been restored from the
	// call log yet. Callers retry after LedgerNotReadyRetry.
	ErrLedgerNotReady = errors.New("quota: ledger not ready")
)

// LedgerNotReadyRetry is how long a caller waits when the rolling ledger is
// not ready. Acquire maps ErrLedgerNotReady to this delay.
const LedgerNotReadyRetry = time.Second

// Valid reports whether c names a supported lane.
func (c Class) Valid() bool {
	return c == Interactive || c == Person || c == Photo
}
