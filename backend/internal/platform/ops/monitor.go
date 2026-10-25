package ops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"
)

// Event names sent to the observability stream.
const (
	HealthEvent    = "crawl.health"
	HealthKeyEvent = "crawl.health.key"
)

// DefaultInterval is how often the health line is logged.
const DefaultInterval = time.Minute

// DefaultIdleAfter is how long pending work may stay without a ready item
// before the monitor warns.
const DefaultIdleAfter = 5 * time.Minute

// Event is one structured record for the observability stream.
type Event map[string]any

// Sink delivers events in one batch.
type Sink interface {
	Send(context.Context, []Event) error
}

// Monitor logs the crawl health line every Interval, ships it to Sink and
// warns when pending work has had no ready item for IdleAfter.
type Monitor struct {
	Service   Service
	Sink      Sink
	Logger    *slog.Logger
	Node      string
	Interval  time.Duration
	IdleAfter time.Duration
	Now       func() time.Time

	idleSince time.Time
}

// Run ticks immediately and then every Interval until ctx ends. Failures are
// logged and never stop the loop.
func (m *Monitor) Run(ctx context.Context) {
	interval := m.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := m.Tick(ctx); err != nil && ctx.Err() == nil {
			m.logger().ErrorContext(ctx, "crawl health: report failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick reads the status once, logs it, ships it and raises the idle warning.
func (m *Monitor) Tick(ctx context.Context) error {
	if ctx == nil {
		return errors.New("ops: monitor context is required")
	}
	status, err := m.Service.Status(ctx)
	if err != nil {
		return err
	}
	logger := m.logger()
	logger.InfoContext(ctx, "crawl health", logAttrs(status)...)
	if status.CredentialDecryptionFailures > 0 {
		logger.WarnContext(ctx, "crawl credentials cannot be decrypted; check configured key IDs and stored envelope metadata", "credential_decryption_failures", status.CredentialDecryptionFailures)
	}
	if m.idleExpired(status) {
		logger.WarnContext(ctx, "crawl idle: pending items but no ready jobs",
			"reason", status.IdleReason,
			"pending_items", status.PendingItems,
			"idle_for", status.GeneratedAt.Sub(m.idleSince).Round(time.Second).String(),
			"oldest_pending_age", status.OldestPendingAge.Round(time.Second).String(),
		)
	}
	if m.Sink == nil {
		return nil
	}
	if err := m.Sink.Send(ctx, Events(status, m.Node)); err != nil {
		logger.WarnContext(ctx, "crawl health: ship to observability failed", "error", err)
	}
	return nil
}

func (m *Monitor) idleExpired(status Status) bool {
	if !status.Idle() {
		m.idleSince = time.Time{}
		return false
	}
	if m.idleSince.IsZero() {
		m.idleSince = status.GeneratedAt
	}
	after := m.IdleAfter
	if after <= 0 {
		after = DefaultIdleAfter
	}
	return status.GeneratedAt.Sub(m.idleSince) >= after
}

func (m *Monitor) logger() *slog.Logger {
	if m.Logger != nil {
		return m.Logger
	}
	return slog.Default()
}

func logAttrs(s Status) []any {
	attrs := []any{
		"people", s.Counts.People,
		"photos", s.Counts.Photos,
		"photo_sizes", s.Counts.PhotoSizes,
		"photo_sizes_pending", s.PhotoSizesJobs.Pending,
		"photo_sizes_ready", s.PhotoSizesJobs.Ready,
		"photo_sizes_running", s.PhotoSizesJobs.Running,
		"favorites", s.Counts.Favorites,
		"albums", s.Counts.Albums,
		"galleries", s.Counts.Galleries,
		"groups", s.Counts.Groups,
		slog.Group("work_items", mapAttrs(s.WorkItems)...),
		"ready_jobs", s.ReadyJobs,
		slog.Group("river_jobs", mapAttrs(s.RiverJobs)...),
		"calls_last_hour", s.CallsLastHour,
		"calls_last_10min", s.CallsLast10Min,
	}
	keys := make([]any, 0, len(s.Keys))
	for _, key := range s.Keys {
		keys = append(keys, slog.Group("key_"+strconv.FormatInt(key.KeyID, 10),
			"calls_last_hour", key.CallsLastHour, "used", key.Used(), "remaining", key.Remaining(), "limit", key.Limit))
	}
	attrs = append(attrs, slog.Group("keys", keys...),
		"pending_items", s.PendingItems,
		"oldest_pending_age", s.OldestPendingAge.Round(time.Second).String(),
		"failed_work_items", s.FailedWorkItems,
		"credential_decryption_failures", s.CredentialDecryptionFailures,
		"dead_jobs", s.DeadJobs,
		"token_status", s.TokenStatus,
	)
	return attrs
}

func mapAttrs(values map[string]int64) []any {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	attrs := make([]any, 0, len(names))
	for _, name := range names {
		attrs = append(attrs, slog.Int64(name, values[name]))
	}
	return attrs
}

// Events renders the status as flat records for the observability stream: one
// crawl.health record plus one crawl.health.key record per API key. The alert
// rules in docs/ops-monitoring.md read these field names.
func Events(s Status, node string) []Event {
	stamp := s.GeneratedAt.UnixMicro()
	health := Event{
		"_timestamp":                     stamp,
		"event":                          HealthEvent,
		"node":                           node,
		"people":                         s.Counts.People,
		"photos":                         s.Counts.Photos,
		"photo_sizes":                    s.Counts.PhotoSizes,
		"photo_sizes_pending":            s.PhotoSizesJobs.Pending,
		"photo_sizes_ready":              s.PhotoSizesJobs.Ready,
		"photo_sizes_running":            s.PhotoSizesJobs.Running,
		"favorites":                      s.Counts.Favorites,
		"albums":                         s.Counts.Albums,
		"galleries":                      s.Counts.Galleries,
		"groups":                         s.Counts.Groups,
		"ready_jobs":                     s.ReadyJobs,
		"calls_last_hour":                s.CallsLastHour,
		"calls_last_10min":               s.CallsLast10Min,
		"pending_items":                  s.PendingItems,
		"oldest_pending_sec":             int64(s.OldestPendingAge / time.Second),
		"failed_work_items":              s.FailedWorkItems,
		"dead_jobs":                      s.DeadJobs,
		"failed_total":                   s.FailedWorkItems + s.DeadJobs,
		"token_status":                   s.TokenStatus,
		"tokens_revoked":                 s.Tokens[TokenRevoked],
		"idle_reason":                    s.IdleReason,
		"credential_decryption_failures": s.CredentialDecryptionFailures,
	}
	for state, count := range s.WorkItems {
		health["work_items_"+state] = count
	}
	for state, count := range s.RiverJobs {
		health["river_jobs_"+state] = count
	}
	events := []Event{health}
	for _, key := range s.Keys {
		events = append(events, Event{
			"_timestamp":      stamp,
			"event":           HealthKeyEvent,
			"node":            node,
			"key_id":          key.KeyID,
			"key_status":      key.Status,
			"calls_last_hour": key.CallsLastHour,
			"used":            key.Used(),
			"remaining":       key.Remaining(),
			"limit":           key.Limit,
		})
	}
	return events
}

// String summarises the status for diagnostics.
func (s Status) String() string {
	return fmt.Sprintf("pending=%d ready=%d calls10m=%d token=%s", s.PendingItems, s.ReadyJobs, s.CallsLast10Min, s.TokenStatus)
}
