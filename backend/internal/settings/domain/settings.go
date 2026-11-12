// Package domain defines XFlickr's application and per-user settings.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Effective contains the merged, validated settings for one user. Durations
// are stored in JSON as integer seconds and converted to time.Duration here.
type Effective struct {
	MaxDepth                  int
	ChangeCheckInterval       time.Duration
	RestrictedRecheckInterval time.Duration
	ReconcileInterval         time.Duration
	ContactsRefreshInterval   time.Duration
	DailyCallLimit            int64 // zero means unlimited
	UsableHourlyQuota         int64
	QuotaReserve              int64
	PerSecondRate             int
	Burst                     int
	PersonLaneShare           float64
	PhotoLaneFloorShare       float64
	ZipTTL                    time.Duration
	RetryMax                  int
	RetryBackoff              time.Duration
	NodeCooldown              time.Duration
	GroupPoolsEnabled         bool
	PhotoInfoEnabled          bool
	PhotoExifEnabled          bool
	PhotoContextsEnabled      bool
	PhotoCommentsEnabled      bool
	PhotoFavoritesEnabled     bool
	PerKeyReadyCap            int
	NodeReadyCap              int
	MediaReadyCap             int
	AIReadyCap                int
	SyncReadyCap              int
	LeaseTimeout              time.Duration
	ScoreWeightPlan           float64
	ScoreWeightDirectAction   float64
	ScoreWeightStarred        float64
	ScoreWeightDepth          float64
	ScoreWeightAgeMinutes     float64
	ScoreAgingBatchSize       int
	QueueReclaimBatchSize     int
	OpsHealthInterval         time.Duration
	OpsHealthStatementTimeout time.Duration
	APICallLogRetentionDays   int
	APICallLogDryRun          bool
	QueueCompletedRetention   time.Duration
	QueueDiscardedRetention   time.Duration
}

// Merge combines application defaults with one user's overrides.
// An override is dropped and reported when overriding was turned off, or when
// the stored value is outside the current cap or schema. The application
// default is used for that key. This keeps a read working after an
// administrator tightens a cap while older rows remain. A write still rejects
// those values through CheckUserOverride.
func Merge(snapshot Snapshot) (Effective, []DroppedOverride, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return Effective{}, nil, err
	}
	values, dropped, err := effectiveValues(snapshot)
	if err != nil {
		return Effective{}, nil, err
	}
	var result Effective
	for _, field := range effectiveFields {
		if err := field(values, &result); err != nil {
			return Effective{}, nil, err
		}
	}
	return result, dropped, nil
}

// validateSnapshot checks the application settings of a snapshot against the registered keys.
func validateSnapshot(snapshot Snapshot) error {
	// Extra rows are ignored on purpose: a database that is newer than this
	// binary (a migration added a setting) or a rollback to the previous binary
	// must not stop the crawl. Only a missing registered key is an error.
	for key, setting := range snapshot.Settings {
		if _, known := schemas[key]; !known {
			continue
		}
		if setting.Key != key {
			return fmt.Errorf("settings: application setting map key %q does not match row key %q", key, setting.Key)
		}
		if err := ValidateDefinition(setting); err != nil {
			return err
		}
	}
	for key := range schemas {
		if _, ok := snapshot.Settings[key]; !ok {
			return fmt.Errorf("settings: missing application setting %q", key)
		}
	}
	return nil
}

// effectiveValues picks, per registered key, the application value or the user override, and reports the dropped overrides.
func effectiveValues(snapshot Snapshot) (map[Key]json.RawMessage, []DroppedOverride, error) {
	values := make(map[Key]json.RawMessage, len(schemas))
	for key, setting := range snapshot.Settings {
		if _, known := schemas[key]; known {
			values[key] = setting.Value
		}
	}
	overrideKeys := make([]Key, 0, len(snapshot.Overrides))
	for key := range snapshot.Overrides {
		overrideKeys = append(overrideKeys, key)
	}
	slices.Sort(overrideKeys)

	var dropped []DroppedOverride
	for _, key := range overrideKeys {
		if _, ok := schemas[key]; !ok {
			return nil, nil, &ValidationError{Key: key, Cause: "is not a registered setting key"}
		}
		setting := snapshot.Settings[key]
		if !setting.UserOverridable {
			dropped = append(dropped, DroppedOverride{Key: key, Reason: "is not user-overridable"})
			continue
		}
		if err := ValidateValue(key, snapshot.Overrides[key], setting.Min, setting.Max); err != nil {
			dropped = append(dropped, DroppedOverride{Key: key, Reason: dropReason(err)})
			continue
		}
		values[key] = snapshot.Overrides[key]
	}
	return values, dropped, nil
}

func dropReason(err error) string {
	var validation *ValidationError
	if errors.As(err, &validation) && validation.Cause != "" {
		return validation.Cause
	}
	return err.Error()
}

// ClampHealthInterval enforces ops.health_interval bounds (10s–1h).
func ClampHealthInterval(value time.Duration) time.Duration {
	const min = 10 * time.Second
	const max = time.Hour
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampHealthTimeout enforces ops.health_statement_timeout bounds (1s–60s).
func ClampHealthTimeout(value time.Duration) time.Duration {
	const min = time.Second
	const max = time.Minute
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
