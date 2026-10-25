// Package ops reports crawl health: one snapshot of counts, queues, quota and
// token state, used by the periodic "crawl health" log line, the admin status
// endpoint and the OpenObserve stream.
package ops

import (
	"context"
	"errors"
	"sort"
	"time"
)

// Idle reasons explain why pending work is not becoming ready.
const (
	ReasonNoKey          = "no_key"
	ReasonQuotaExhausted = "quota_exhausted"
	ReasonNotBeforeLater = "not_before_in_future"
	ReasonUnknown        = "unknown"
)

// Token status values, from the least to the most severe.
const (
	TokenNone      = "none"
	TokenOK        = "ok"
	TokenReconnect = "reconnect_required" //nolint:gosec // status name, not a credential
	TokenRevoked   = "revoked"
)

// Counts are the catalog table sizes. Large tables may be estimates.
type Counts struct {
	People     int64
	Photos     int64
	PhotoSizes int64
	Favorites  int64
	Albums     int64
	Galleries  int64
	Groups     int64
}

// KeyStatus is the call usage of one API key in the last hour.
type KeyStatus struct {
	KeyID         int64
	Label         string
	Status        string
	CallsLastHour int64
	Limit         int64
}

// Used is the number of calls spent in the hourly window.
func (k KeyStatus) Used() int64 { return k.CallsLastHour }

// Remaining is the budget left in the hourly window, never negative.
func (k KeyStatus) Remaining() int64 {
	if left := k.Limit - k.CallsLastHour; left > 0 {
		return left
	}
	return 0
}

// Pending describes the work items waiting to become ready.
type Pending struct {
	Total          int64
	WithoutKey     int64
	NotBeforeLater int64
	Oldest         time.Time
}

// Snapshot is the raw data read from storage in one pass.
type Snapshot struct {
	Counts                       Counts
	WorkItems                    map[string]int64
	RiverJobs                    map[string]int64
	PhotoSizesJobs               LaneJobs
	Keys                         []KeyStatus
	CallsLast10Min               int64
	Pending                      Pending
	FailedWorkItems              int64
	DeadJobs                     int64
	Tokens                       map[string]int64
	CredentialDecryptionFailures int64
}

// LaneJobs reports durable size work waiting for execution, ready to run, and running.
type LaneJobs struct {
	Pending int64
	Ready   int64
	Running int64
}

// Store reads a Snapshot.
type Store interface {
	Snapshot(context.Context) (Snapshot, error)
}

// Status is the derived crawl health report.
type Status struct {
	GeneratedAt                  time.Time
	Counts                       Counts
	WorkItems                    map[string]int64
	ReadyJobs                    int64
	RiverJobs                    map[string]int64
	PhotoSizesJobs               LaneJobs
	Keys                         []KeyStatus
	CallsLastHour                int64
	CallsLast10Min               int64
	PendingItems                 int64
	OldestPendingAge             time.Duration
	FailedWorkItems              int64
	DeadJobs                     int64
	Tokens                       map[string]int64
	TokenStatus                  string
	CredentialDecryptionFailures int64
	// IdleReason is set when pending items exist but none is ready.
	IdleReason string
}

// Idle reports whether pending work exists while nothing is ready.
func (s Status) Idle() bool { return s.PendingItems > 0 && s.ReadyJobs == 0 }

// Service builds Status values from a Store.
type Service struct {
	Store Store
	Now   func() time.Time
}

// Status reads storage and derives the report.
func (s Service) Status(ctx context.Context) (Status, error) {
	if ctx == nil || s.Store == nil {
		return Status{}, errors.New("ops: status store is required")
	}
	snapshot, err := s.Store.Snapshot(ctx)
	if err != nil {
		return Status{}, err
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	status := Status{
		GeneratedAt:                  now.UTC(),
		Counts:                       snapshot.Counts,
		WorkItems:                    copyMap(snapshot.WorkItems),
		ReadyJobs:                    snapshot.WorkItems["ready"],
		RiverJobs:                    copyMap(snapshot.RiverJobs),
		PhotoSizesJobs:               snapshot.PhotoSizesJobs,
		Keys:                         append([]KeyStatus(nil), snapshot.Keys...),
		CallsLast10Min:               snapshot.CallsLast10Min,
		PendingItems:                 snapshot.Pending.Total,
		FailedWorkItems:              snapshot.FailedWorkItems,
		DeadJobs:                     snapshot.DeadJobs,
		Tokens:                       copyMap(snapshot.Tokens),
		TokenStatus:                  tokenStatus(snapshot.Tokens),
		CredentialDecryptionFailures: snapshot.CredentialDecryptionFailures,
	}
	sort.Slice(status.Keys, func(i, j int) bool { return status.Keys[i].KeyID < status.Keys[j].KeyID })
	for _, key := range status.Keys {
		status.CallsLastHour += key.CallsLastHour
	}
	if snapshot.Pending.Total > 0 && !snapshot.Pending.Oldest.IsZero() {
		if age := now.Sub(snapshot.Pending.Oldest); age > 0 {
			status.OldestPendingAge = age
		}
	}
	if status.Idle() {
		status.IdleReason = idleReason(snapshot.Pending, status.Keys)
	}
	return status, nil
}

func idleReason(pending Pending, keys []KeyStatus) string {
	usable := 0
	exhausted := 0
	for _, key := range keys {
		if key.Status != "" && key.Status != "valid" {
			continue
		}
		usable++
		if key.Remaining() == 0 {
			exhausted++
		}
	}
	switch {
	case usable == 0 || pending.WithoutKey >= pending.Total:
		return ReasonNoKey
	case exhausted == usable:
		return ReasonQuotaExhausted
	case pending.NotBeforeLater >= pending.Total-pending.WithoutKey:
		return ReasonNotBeforeLater
	default:
		return ReasonUnknown
	}
}

func tokenStatus(tokens map[string]int64) string {
	switch {
	case tokens[TokenRevoked] > 0:
		return TokenRevoked
	case tokens[TokenReconnect] > 0:
		return TokenReconnect
	case tokens[TokenOK] > 0:
		return TokenOK
	default:
		return TokenNone
	}
}

func copyMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
