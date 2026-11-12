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
//
//nolint:cyclop // per-setting merge rules; tracked for a table-driven refactor
func Merge(snapshot Snapshot) (Effective, []DroppedOverride, error) {
	// Extra rows are ignored on purpose: a database that is newer than this
	// binary (a migration added a setting) or a rollback to the previous binary
	// must not stop the crawl. Only a missing registered key is an error.
	for key, setting := range snapshot.Settings {
		if _, known := schemas[key]; !known {
			continue
		}
		if setting.Key != key {
			return Effective{}, nil, fmt.Errorf("settings: application setting map key %q does not match row key %q", key, setting.Key)
		}
		if err := ValidateDefinition(setting); err != nil {
			return Effective{}, nil, err
		}
	}
	for key := range schemas {
		if _, ok := snapshot.Settings[key]; !ok {
			return Effective{}, nil, fmt.Errorf("settings: missing application setting %q", key)
		}
	}

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
			return Effective{}, nil, &ValidationError{Key: key, Cause: "is not a registered setting key"}
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

	var result Effective
	var err error
	if result.MaxDepth, err = integer(values[MaxDepth]); err != nil {
		return Effective{}, nil, invalidStored(MaxDepth, err)
	}
	if result.ChangeCheckInterval, err = duration(values[ChangeCheckInterval]); err != nil {
		return Effective{}, nil, invalidStored(ChangeCheckInterval, err)
	}
	if result.RestrictedRecheckInterval, err = duration(values[RestrictedRecheckInterval]); err != nil {
		return Effective{}, nil, invalidStored(RestrictedRecheckInterval, err)
	}
	if result.ReconcileInterval, err = duration(values[ReconcileInterval]); err != nil {
		return Effective{}, nil, invalidStored(ReconcileInterval, err)
	}
	if result.ContactsRefreshInterval, err = duration(values[ContactsRefreshInterval]); err != nil {
		return Effective{}, nil, invalidStored(ContactsRefreshInterval, err)
	}
	if result.DailyCallLimit, err = integer64(values[DailyCallLimit]); err != nil {
		return Effective{}, nil, invalidStored(DailyCallLimit, err)
	}
	if result.UsableHourlyQuota, err = integer64(values[UsableHourlyQuota]); err != nil {
		return Effective{}, nil, invalidStored(UsableHourlyQuota, err)
	}
	if result.QuotaReserve, err = integer64(values[QuotaReserve]); err != nil {
		return Effective{}, nil, invalidStored(QuotaReserve, err)
	}
	if result.PerSecondRate, err = integer(values[PerSecondRate]); err != nil {
		return Effective{}, nil, invalidStored(PerSecondRate, err)
	}
	if result.Burst, err = integer(values[Burst]); err != nil {
		return Effective{}, nil, invalidStored(Burst, err)
	}
	if result.PersonLaneShare, err = decimal(values[PersonLaneShare]); err != nil {
		return Effective{}, nil, invalidStored(PersonLaneShare, err)
	}
	if result.PhotoLaneFloorShare, err = decimal(values[PhotoLaneFloorShare]); err != nil {
		return Effective{}, nil, invalidStored(PhotoLaneFloorShare, err)
	}
	if result.ZipTTL, err = duration(values[ZipTTL]); err != nil {
		return Effective{}, nil, invalidStored(ZipTTL, err)
	}
	if result.RetryMax, err = integer(values[RetryMax]); err != nil {
		return Effective{}, nil, invalidStored(RetryMax, err)
	}
	if result.RetryBackoff, err = duration(values[RetryBackoff]); err != nil {
		return Effective{}, nil, invalidStored(RetryBackoff, err)
	}
	if result.NodeCooldown, err = duration(values[NodeCooldown]); err != nil {
		return Effective{}, nil, invalidStored(NodeCooldown, err)
	}
	// Every definition and override was validated above, so these values are
	// guaranteed JSON boolean literals and cannot fail conversion here.
	result.GroupPoolsEnabled = validatedBoolean(values[GroupPoolsEnabled])
	result.PhotoInfoEnabled = validatedBoolean(values[PhotoInfoEnabled])
	result.PhotoExifEnabled = validatedBoolean(values[PhotoExifEnabled])
	result.PhotoContextsEnabled = validatedBoolean(values[PhotoContextsEnabled])
	result.PhotoCommentsEnabled = validatedBoolean(values[PhotoCommentsEnabled])
	result.PhotoFavoritesEnabled = validatedBoolean(values[PhotoFavoritesEnabled])
	if result.PerKeyReadyCap, err = integer(values[PerKeyReadyCap]); err != nil {
		return Effective{}, nil, invalidStored(PerKeyReadyCap, err)
	}
	if result.NodeReadyCap, err = integer(values[NodeReadyCap]); err != nil {
		return Effective{}, nil, invalidStored(NodeReadyCap, err)
	}
	if result.MediaReadyCap, err = integer(values[MediaReadyCap]); err != nil {
		return Effective{}, nil, invalidStored(MediaReadyCap, err)
	}
	if result.AIReadyCap, err = integer(values[AIReadyCap]); err != nil {
		return Effective{}, nil, invalidStored(AIReadyCap, err)
	}
	if result.SyncReadyCap, err = integer(values[SyncReadyCap]); err != nil {
		return Effective{}, nil, invalidStored(SyncReadyCap, err)
	}
	if result.LeaseTimeout, err = duration(values[LeaseTimeout]); err != nil {
		return Effective{}, nil, invalidStored(LeaseTimeout, err)
	}
	if result.ScoreWeightPlan, err = decimal(values[ScoreWeightPlan]); err != nil {
		return Effective{}, nil, invalidStored(ScoreWeightPlan, err)
	}
	if result.ScoreWeightDirectAction, err = decimal(values[ScoreWeightDirectAction]); err != nil {
		return Effective{}, nil, invalidStored(ScoreWeightDirectAction, err)
	}
	if result.ScoreWeightStarred, err = decimal(values[ScoreWeightStarred]); err != nil {
		return Effective{}, nil, invalidStored(ScoreWeightStarred, err)
	}
	if result.ScoreWeightDepth, err = decimal(values[ScoreWeightDepth]); err != nil {
		return Effective{}, nil, invalidStored(ScoreWeightDepth, err)
	}
	if result.ScoreWeightAgeMinutes, err = decimal(values[ScoreWeightAgeMinutes]); err != nil {
		return Effective{}, nil, invalidStored(ScoreWeightAgeMinutes, err)
	}
	if result.ScoreAgingBatchSize, err = integer(values[ScoreAgingBatchSize]); err != nil {
		return Effective{}, nil, invalidStored(ScoreAgingBatchSize, err)
	}
	if result.QueueReclaimBatchSize, err = integer(values[QueueReclaimBatchSize]); err != nil {
		return Effective{}, nil, invalidStored(QueueReclaimBatchSize, err)
	}
	if result.OpsHealthInterval, err = duration(values[OpsHealthInterval]); err != nil {
		return Effective{}, nil, invalidStored(OpsHealthInterval, err)
	}
	if result.OpsHealthStatementTimeout, err = duration(values[OpsHealthStatementTimeout]); err != nil {
		return Effective{}, nil, invalidStored(OpsHealthStatementTimeout, err)
	}
	if result.APICallLogRetentionDays, err = integer(values[APICallLogRetentionDays]); err != nil {
		return Effective{}, nil, invalidStored(APICallLogRetentionDays, err)
	}
	result.APICallLogDryRun = validatedBoolean(values[APICallLogDryRun])
	if result.QueueCompletedRetention, err = duration(values[QueueCompletedRetention]); err != nil {
		return Effective{}, nil, invalidStored(QueueCompletedRetention, err)
	}
	if result.QueueDiscardedRetention, err = duration(values[QueueDiscardedRetention]); err != nil {
		return Effective{}, nil, invalidStored(QueueDiscardedRetention, err)
	}
	return result, dropped, nil
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
