package domain

import (
	"encoding/json"
)

// Definition is the application-level value and policy for a setting.
// Numeric cap values use the same units as its JSON value; duration settings
// therefore use seconds.
type Definition struct {
	Key             Key
	Value           json.RawMessage
	UserOverridable bool
	Min             *float64
	Max             *float64
}

// Snapshot is the application settings and one user's overrides read together.
type Snapshot struct {
	Settings  map[Key]Definition
	Overrides map[Key]json.RawMessage
}

// DroppedOverride is a stored user override that a read ignored. The override
// no longer matches the application policy, so the application default is used
// for that key. Writers still reject a new value that violates the policy.
type DroppedOverride struct {
	Key    Key
	Reason string
}

const (
	// ChangedTopic is the transactional outbox topic for a committed settings write.
	ChangedTopic = "settings.changed"

	// ScopeApplication identifies a change to an app_settings row.
	ScopeApplication = "application"
	// ScopeUser identifies a change to a user_settings row.
	ScopeUser = "user"
)

// Change is the JSON payload published with ChangedTopic.
type Change struct {
	Key             Key             `json:"key"`
	Scope           string          `json:"scope"`
	UserID          *int64          `json:"user_id,omitempty"`
	Value           json.RawMessage `json:"value"`
	UserOverridable *bool           `json:"user_overridable,omitempty"`
	Min             *float64        `json:"min,omitempty"`
	Max             *float64        `json:"max,omitempty"`
}

// Defaults returns the seeded defaults and admin caps as domain values.
func Defaults() []Definition {
	return []Definition{
		{Key: MaxDepth, Value: json.RawMessage("2"), UserOverridable: true, Min: floatPointer(1), Max: floatPointer(4)},
		{Key: ChangeCheckInterval, Value: json.RawMessage("86400"), UserOverridable: true},
		{Key: RestrictedRecheckInterval, Value: json.RawMessage("2592000"), UserOverridable: false},
		{Key: ReconcileInterval, Value: json.RawMessage("2592000"), UserOverridable: true},
		{Key: ContactsRefreshInterval, Value: json.RawMessage("86400"), UserOverridable: true, Min: floatPointer(3600), Max: floatPointer(30 * 24 * 3600)},
		{Key: DailyCallLimit, Value: json.RawMessage("0"), UserOverridable: true},
		{Key: UsableHourlyQuota, Value: json.RawMessage("3400"), UserOverridable: false, Min: floatPointer(1)},
		{Key: QuotaReserve, Value: json.RawMessage("100"), UserOverridable: true},
		{Key: PerSecondRate, Value: json.RawMessage("4"), UserOverridable: true},
		{Key: Burst, Value: json.RawMessage("5"), UserOverridable: true},
		{Key: PersonLaneShare, Value: json.RawMessage("0.30"), UserOverridable: true},
		{Key: PhotoLaneFloorShare, Value: json.RawMessage("0.10"), UserOverridable: false},
		{Key: ZipTTL, Value: json.RawMessage("86400"), UserOverridable: true},
		{Key: RetryMax, Value: json.RawMessage("5"), UserOverridable: true},
		{Key: RetryBackoff, Value: json.RawMessage("30"), UserOverridable: true},
		{Key: NodeCooldown, Value: json.RawMessage("2700"), UserOverridable: true},
		{Key: GroupPoolsEnabled, Value: json.RawMessage("false"), UserOverridable: true},
		{Key: PhotoInfoEnabled, Value: json.RawMessage("true"), UserOverridable: false},
		{Key: PhotoExifEnabled, Value: json.RawMessage("true"), UserOverridable: false},
		{Key: PhotoContextsEnabled, Value: json.RawMessage("true"), UserOverridable: false},
		{Key: PhotoCommentsEnabled, Value: json.RawMessage("false"), UserOverridable: false},
		{Key: PhotoFavoritesEnabled, Value: json.RawMessage("false"), UserOverridable: false},
		{Key: RegistrationOpen, Value: json.RawMessage("false"), UserOverridable: false},
		{Key: PerKeyReadyCap, Value: json.RawMessage("100"), UserOverridable: false, Min: floatPointer(0)},
		{Key: NodeReadyCap, Value: json.RawMessage("500"), UserOverridable: false, Min: floatPointer(0)},
		{Key: MediaReadyCap, Value: json.RawMessage("100"), UserOverridable: false, Min: floatPointer(0)},
		{Key: AIReadyCap, Value: json.RawMessage("100"), UserOverridable: false, Min: floatPointer(0)},
		{Key: SyncReadyCap, Value: json.RawMessage("100"), UserOverridable: false, Min: floatPointer(0)},
		{Key: LeaseTimeout, Value: json.RawMessage("120"), UserOverridable: false, Min: floatPointer(1)},
		{Key: ScoreWeightPlan, Value: json.RawMessage("10"), UserOverridable: false, Min: floatPointer(0)},
		{Key: ScoreWeightDirectAction, Value: json.RawMessage("100"), UserOverridable: false, Min: floatPointer(0)},
		{Key: ScoreWeightStarred, Value: json.RawMessage("20"), UserOverridable: false, Min: floatPointer(0)},
		{Key: ScoreWeightDepth, Value: json.RawMessage("5"), UserOverridable: false, Min: floatPointer(0)},
		{Key: ScoreWeightAgeMinutes, Value: json.RawMessage("0.1"), UserOverridable: false, Min: floatPointer(0)},
		{Key: ScoreAgingBatchSize, Value: json.RawMessage("500"), UserOverridable: false, Min: floatPointer(1)},
		{Key: QueueReclaimBatchSize, Value: json.RawMessage("100"), UserOverridable: false, Min: floatPointer(1)},
		{Key: OpsHealthInterval, Value: json.RawMessage("60"), UserOverridable: false, Min: floatPointer(10), Max: floatPointer(3600)},
		{Key: OpsHealthStatementTimeout, Value: json.RawMessage("5"), UserOverridable: false, Min: floatPointer(1), Max: floatPointer(60)},
		{Key: APICallLogRetentionDays, Value: json.RawMessage("30"), UserOverridable: false, Min: floatPointer(7), Max: floatPointer(365)},
		{Key: APICallLogDryRun, Value: json.RawMessage("false"), UserOverridable: false},
		{Key: MetricsRetentionDays, Value: json.RawMessage("30"), UserOverridable: false, Min: floatPointer(1), Max: floatPointer(365)},
		{Key: QueueCompletedRetention, Value: json.RawMessage("86400"), UserOverridable: false, Min: floatPointer(60), Max: floatPointer(604800)},
		{Key: QueueDiscardedRetention, Value: json.RawMessage("604800"), UserOverridable: false, Min: floatPointer(3600), Max: floatPointer(2592000)},
	}
}

// DefaultSnapshot returns the seeded application settings and no user overrides.
func DefaultSnapshot() Snapshot {
	settings := make(map[Key]Definition, len(schemas))
	for _, setting := range Defaults() {
		settings[setting.Key] = setting
	}
	return Snapshot{Settings: settings}
}
