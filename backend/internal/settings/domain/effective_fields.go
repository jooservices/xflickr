package domain

import (
	"encoding/json"
	"time"
)

// effectiveField reads one registered setting from the merged values into the Effective struct.
type effectiveField func(values map[Key]json.RawMessage, result *Effective) error

// decoded decodes the stored value of key with decode and stores it where target points; a value that cannot
// be decoded is an invalid stored value.
func decoded[T any](key Key, decode func([]byte) (T, error), target func(*Effective) *T) effectiveField {
	return func(values map[Key]json.RawMessage, result *Effective) error {
		value, err := decode(values[key])
		if err != nil {
			return invalidStored(key, err)
		}
		*target(result) = value
		return nil
	}
}

// flag stores a boolean setting. Every definition and override was validated before the merge, so the stored
// value is a JSON boolean literal and the conversion cannot fail.
func flag(key Key, target func(*Effective) *bool) effectiveField {
	return func(values map[Key]json.RawMessage, result *Effective) error {
		*target(result) = validatedBoolean(values[key])
		return nil
	}
}

// effectiveFields lists every setting once, in the order in which a bad stored value is reported.
var effectiveFields = []effectiveField{
	decoded(MaxDepth, integer, func(e *Effective) *int { return &e.MaxDepth }),
	decoded(ChangeCheckInterval, duration, func(e *Effective) *time.Duration { return &e.ChangeCheckInterval }),
	decoded(RestrictedRecheckInterval, duration, func(e *Effective) *time.Duration { return &e.RestrictedRecheckInterval }),
	decoded(ReconcileInterval, duration, func(e *Effective) *time.Duration { return &e.ReconcileInterval }),
	decoded(ContactsRefreshInterval, duration, func(e *Effective) *time.Duration { return &e.ContactsRefreshInterval }),
	decoded(DailyCallLimit, integer64, func(e *Effective) *int64 { return &e.DailyCallLimit }),
	decoded(UsableHourlyQuota, integer64, func(e *Effective) *int64 { return &e.UsableHourlyQuota }),
	decoded(QuotaReserve, integer64, func(e *Effective) *int64 { return &e.QuotaReserve }),
	decoded(PerSecondRate, integer, func(e *Effective) *int { return &e.PerSecondRate }),
	decoded(Burst, integer, func(e *Effective) *int { return &e.Burst }),
	decoded(PersonLaneShare, decimal, func(e *Effective) *float64 { return &e.PersonLaneShare }),
	decoded(PhotoLaneFloorShare, decimal, func(e *Effective) *float64 { return &e.PhotoLaneFloorShare }),
	decoded(ZipTTL, duration, func(e *Effective) *time.Duration { return &e.ZipTTL }),
	decoded(RetryMax, integer, func(e *Effective) *int { return &e.RetryMax }),
	decoded(RetryBackoff, duration, func(e *Effective) *time.Duration { return &e.RetryBackoff }),
	decoded(NodeCooldown, duration, func(e *Effective) *time.Duration { return &e.NodeCooldown }),
	flag(GroupPoolsEnabled, func(e *Effective) *bool { return &e.GroupPoolsEnabled }),
	flag(PhotoInfoEnabled, func(e *Effective) *bool { return &e.PhotoInfoEnabled }),
	flag(PhotoExifEnabled, func(e *Effective) *bool { return &e.PhotoExifEnabled }),
	flag(PhotoContextsEnabled, func(e *Effective) *bool { return &e.PhotoContextsEnabled }),
	flag(PhotoCommentsEnabled, func(e *Effective) *bool { return &e.PhotoCommentsEnabled }),
	flag(PhotoFavoritesEnabled, func(e *Effective) *bool { return &e.PhotoFavoritesEnabled }),
	decoded(PerKeyReadyCap, integer, func(e *Effective) *int { return &e.PerKeyReadyCap }),
	decoded(NodeReadyCap, integer, func(e *Effective) *int { return &e.NodeReadyCap }),
	decoded(MediaReadyCap, integer, func(e *Effective) *int { return &e.MediaReadyCap }),
	decoded(AIReadyCap, integer, func(e *Effective) *int { return &e.AIReadyCap }),
	decoded(SyncReadyCap, integer, func(e *Effective) *int { return &e.SyncReadyCap }),
	decoded(LeaseTimeout, duration, func(e *Effective) *time.Duration { return &e.LeaseTimeout }),
	decoded(ScoreWeightPlan, decimal, func(e *Effective) *float64 { return &e.ScoreWeightPlan }),
	decoded(ScoreWeightDirectAction, decimal, func(e *Effective) *float64 { return &e.ScoreWeightDirectAction }),
	decoded(ScoreWeightStarred, decimal, func(e *Effective) *float64 { return &e.ScoreWeightStarred }),
	decoded(ScoreWeightDepth, decimal, func(e *Effective) *float64 { return &e.ScoreWeightDepth }),
	decoded(ScoreWeightAgeMinutes, decimal, func(e *Effective) *float64 { return &e.ScoreWeightAgeMinutes }),
	decoded(ScoreAgingBatchSize, integer, func(e *Effective) *int { return &e.ScoreAgingBatchSize }),
	decoded(QueueReclaimBatchSize, integer, func(e *Effective) *int { return &e.QueueReclaimBatchSize }),
	decoded(OpsHealthInterval, duration, func(e *Effective) *time.Duration { return &e.OpsHealthInterval }),
	decoded(OpsHealthStatementTimeout, duration, func(e *Effective) *time.Duration { return &e.OpsHealthStatementTimeout }),
	decoded(APICallLogRetentionDays, integer, func(e *Effective) *int { return &e.APICallLogRetentionDays }),
	flag(APICallLogDryRun, func(e *Effective) *bool { return &e.APICallLogDryRun }),
	decoded(QueueCompletedRetention, duration, func(e *Effective) *time.Duration { return &e.QueueCompletedRetention }),
	decoded(QueueDiscardedRetention, duration, func(e *Effective) *time.Duration { return &e.QueueDiscardedRetention }),
}
