package domain

import (
	"math"
	"time"
)

// Key is a typed settings key persisted in app_settings and user_settings.
type Key string

const (
	MaxDepth                  Key = "max_depth"
	ChangeCheckInterval       Key = "change_check_interval"
	RestrictedRecheckInterval Key = "restricted_recheck_interval"
	ReconcileInterval         Key = "reconcile_interval"
	ContactsRefreshInterval   Key = "contacts_refresh_interval"
	DailyCallLimit            Key = "daily_call_limit"
	UsableHourlyQuota         Key = "usable_hourly_quota"
	QuotaReserve              Key = "quota_reserve"
	PerSecondRate             Key = "per_second_rate"
	Burst                     Key = "burst"
	PersonLaneShare           Key = "person_lane_share"
	PhotoLaneFloorShare       Key = "photo_lane_floor_share"
	ZipTTL                    Key = "zip_ttl"
	RetryMax                  Key = "retry_max"
	RetryBackoff              Key = "retry_backoff"
	NodeCooldown              Key = "node_cooldown"
	GroupPoolsEnabled         Key = "group_pools_enabled"
	PhotoInfoEnabled          Key = "photo_info_enabled"
	PhotoExifEnabled          Key = "photo_exif_enabled"
	PhotoContextsEnabled      Key = "photo_contexts_enabled"
	PhotoCommentsEnabled      Key = "photo_comments_enabled"
	PhotoFavoritesEnabled     Key = "photo_favorites_enabled"
	RegistrationOpen          Key = "registration_open"
	PerKeyReadyCap            Key = "per_key_ready_cap"
	NodeReadyCap              Key = "node_ready_cap"
	MediaReadyCap             Key = "media_ready_cap"
	AIReadyCap                Key = "ai_ready_cap"
	SyncReadyCap              Key = "sync_ready_cap"
	LeaseTimeout              Key = "lease_timeout"
	ScoreWeightPlan           Key = "score_weight_plan"
	ScoreWeightDirectAction   Key = "score_weight_direct_action"
	ScoreWeightStarred        Key = "score_weight_starred"
	ScoreWeightDepth          Key = "score_weight_depth"
	ScoreWeightAgeMinutes     Key = "score_weight_age_minutes"
	ScoreAgingBatchSize       Key = "score_aging_batch_size"
	QueueReclaimBatchSize     Key = "queue.reclaim_batch_size"
	OpsHealthInterval         Key = "ops.health_interval"
	OpsHealthStatementTimeout Key = "ops.health_statement_timeout"
	APICallLogRetentionDays   Key = "maintenance.api_call_log_retention_days"
	APICallLogDryRun          Key = "maintenance.api_call_log_dry_run"
	MetricsRetentionDays      Key = "metrics.retention_days"
	QueueCompletedRetention   Key = "queue.completed_retention"
	QueueDiscardedRetention   Key = "queue.discarded_retention"
)

// JSONSchema describes the JSON value allowed for a setting. The schema
// registry is intentionally small and covers the JSON Schema keywords used
// by these settings.
type JSONSchema struct {
	Type    string   `json:"type"`
	Minimum *float64 `json:"minimum,omitempty"`
	Maximum *float64 `json:"maximum,omitempty"`
}

var (
	minOne      = floatPointer(1)
	minZero     = floatPointer(0)
	maxOne      = floatPointer(1)
	maxDuration = floatPointer(float64(math.MaxInt64 / int64(time.Second)))
)

var schemas = map[Key]JSONSchema{
	MaxDepth:                  {Type: "integer", Minimum: minOne},
	ChangeCheckInterval:       {Type: "integer", Minimum: minOne, Maximum: maxDuration},
	RestrictedRecheckInterval: {Type: "integer", Minimum: minOne, Maximum: maxDuration},
	ReconcileInterval:         {Type: "integer", Minimum: minOne, Maximum: maxDuration},
	ContactsRefreshInterval:   {Type: "integer", Minimum: floatPointer(3600), Maximum: floatPointer(30 * 24 * 3600)},
	DailyCallLimit:            {Type: "integer", Minimum: minZero},
	UsableHourlyQuota:         {Type: "integer", Minimum: minOne},
	QuotaReserve:              {Type: "integer", Minimum: minZero},
	PerSecondRate:             {Type: "integer", Minimum: minOne},
	Burst:                     {Type: "integer", Minimum: minOne},
	PersonLaneShare:           {Type: "number", Minimum: minZero, Maximum: maxOne},
	PhotoLaneFloorShare:       {Type: "number", Minimum: minZero, Maximum: floatPointer(0.5)},
	ZipTTL:                    {Type: "integer", Minimum: minOne, Maximum: maxDuration},
	RetryMax:                  {Type: "integer", Minimum: minZero},
	RetryBackoff:              {Type: "integer", Minimum: minZero, Maximum: maxDuration},
	NodeCooldown:              {Type: "integer", Minimum: minZero, Maximum: maxDuration},
	GroupPoolsEnabled:         {Type: "boolean"},
	PhotoInfoEnabled:          {Type: "boolean"},
	PhotoExifEnabled:          {Type: "boolean"},
	PhotoContextsEnabled:      {Type: "boolean"},
	PhotoCommentsEnabled:      {Type: "boolean"},
	PhotoFavoritesEnabled:     {Type: "boolean"},
	RegistrationOpen:          {Type: "boolean"},
	PerKeyReadyCap:            {Type: "integer", Minimum: minZero},
	NodeReadyCap:              {Type: "integer", Minimum: minZero},
	MediaReadyCap:             {Type: "integer", Minimum: minZero},
	AIReadyCap:                {Type: "integer", Minimum: minZero},
	SyncReadyCap:              {Type: "integer", Minimum: minZero},
	LeaseTimeout:              {Type: "integer", Minimum: minOne, Maximum: maxDuration},
	ScoreWeightPlan:           {Type: "number", Minimum: minZero},
	ScoreWeightDirectAction:   {Type: "number", Minimum: minZero},
	ScoreWeightStarred:        {Type: "number", Minimum: minZero},
	ScoreWeightDepth:          {Type: "number", Minimum: minZero},
	ScoreWeightAgeMinutes:     {Type: "number", Minimum: minZero},
	ScoreAgingBatchSize:       {Type: "integer", Minimum: minOne},
	QueueReclaimBatchSize:     {Type: "integer", Minimum: minOne, Maximum: floatPointer(10000)},
	OpsHealthInterval:         {Type: "integer", Minimum: floatPointer(10), Maximum: floatPointer(3600)},
	OpsHealthStatementTimeout: {Type: "integer", Minimum: floatPointer(1), Maximum: floatPointer(60)},
	APICallLogRetentionDays:   {Type: "integer", Minimum: floatPointer(7), Maximum: floatPointer(365)},
	APICallLogDryRun:          {Type: "boolean"},
	MetricsRetentionDays:      {Type: "integer", Minimum: minOne, Maximum: floatPointer(365)},
	QueueCompletedRetention:   {Type: "integer", Minimum: floatPointer(60), Maximum: floatPointer(604800)},
	QueueDiscardedRetention:   {Type: "integer", Minimum: floatPointer(3600), Maximum: floatPointer(2592000)},
}

// SchemaFor returns the JSON schema associated with a typed key.
func SchemaFor(key Key) (JSONSchema, bool) {
	schema, ok := schemas[key]
	return schema, ok
}

// Keys returns every setting key in stable order.
func Keys() []Key {
	return []Key{
		MaxDepth,
		ChangeCheckInterval,
		ReconcileInterval,
		ContactsRefreshInterval,
		RestrictedRecheckInterval,
		DailyCallLimit,
		UsableHourlyQuota,
		QuotaReserve,
		PerSecondRate,
		Burst,
		PersonLaneShare,
		PhotoLaneFloorShare,
		ZipTTL,
		RetryMax,
		RetryBackoff,
		NodeCooldown,
		GroupPoolsEnabled,
		PhotoInfoEnabled,
		PhotoExifEnabled,
		PhotoContextsEnabled,
		PhotoCommentsEnabled,
		PhotoFavoritesEnabled,
		RegistrationOpen,
		PerKeyReadyCap,
		NodeReadyCap,
		MediaReadyCap,
		AIReadyCap,
		SyncReadyCap,
		LeaseTimeout,
		ScoreWeightPlan, ScoreWeightDirectAction, ScoreWeightStarred, ScoreWeightDepth, ScoreWeightAgeMinutes, ScoreAgingBatchSize, QueueReclaimBatchSize,
		OpsHealthInterval,
		OpsHealthStatementTimeout,
		APICallLogRetentionDays,
		APICallLogDryRun,
		MetricsRetentionDays,
		QueueCompletedRetention,
		QueueDiscardedRetention,
	}
}
