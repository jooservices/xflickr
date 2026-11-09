// Package domain defines XFlickr's application and per-user settings.
package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"time"
)

// Key is a typed settings key persisted in app_settings and user_settings.
type Key string

const (
	MaxDepth                  Key = "max_depth"
	ChangeCheckInterval       Key = "change_check_interval"
	RestrictedRecheckInterval Key = "restricted_recheck_interval"
	ReconcileInterval         Key = "reconcile_interval"
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
	HomeStatsRefreshInterval  Key = "home.stats_refresh_interval"
)

// Effective contains the merged, validated settings for one user. Durations
// are stored in JSON as integer seconds and converted to time.Duration here.
type Effective struct {
	MaxDepth                  int
	ChangeCheckInterval       time.Duration
	RestrictedRecheckInterval time.Duration
	ReconcileInterval         time.Duration
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

// JSONSchema describes the JSON value allowed for a setting. The schema
// registry is intentionally small and covers the JSON Schema keywords used
// by these settings.
type JSONSchema struct {
	Type    string   `json:"type"`
	Minimum *float64 `json:"minimum,omitempty"`
	Maximum *float64 `json:"maximum,omitempty"`
}

// ValidationError names the setting and the schema rule or cap it violates.
type ValidationError struct {
	Key   Key
	Cap   string
	Cause string
}

func (e *ValidationError) Error() string {
	if e == nil || (e.Key == "" && e.Cause == "" && e.Cap == "") {
		return "settings: validation failed"
	}
	if e.Cause != "" {
		return fmt.Sprintf("settings: %s %s", e.Key, e.Cause)
	}
	return fmt.Sprintf("settings: %s violates cap %s", e.Key, e.Cap)
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
	HomeStatsRefreshInterval:  {Type: "integer", Minimum: floatPointer(60), Maximum: floatPointer(86400)},
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
		HomeStatsRefreshInterval,
	}
}

// Defaults returns the seeded defaults and admin caps as domain values.
func Defaults() []Definition {
	return []Definition{
		{Key: MaxDepth, Value: json.RawMessage("2"), UserOverridable: true, Min: floatPointer(1), Max: floatPointer(4)},
		{Key: ChangeCheckInterval, Value: json.RawMessage("86400"), UserOverridable: true},
		{Key: RestrictedRecheckInterval, Value: json.RawMessage("2592000"), UserOverridable: false},
		{Key: ReconcileInterval, Value: json.RawMessage("2592000"), UserOverridable: true},
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
		{Key: HomeStatsRefreshInterval, Value: json.RawMessage("900"), UserOverridable: false, Min: floatPointer(60), Max: floatPointer(86400)},
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

// ValidateDefinition validates a stored default and its configured caps.
func ValidateDefinition(setting Definition) error {
	if !finiteCap(setting.Min) || !finiteCap(setting.Max) {
		return &ValidationError{Key: setting.Key, Cause: "caps must be finite"}
	}
	if setting.Min != nil && setting.Max != nil && *setting.Min > *setting.Max {
		return &ValidationError{Key: setting.Key, Cause: "minimum cap exceeds maximum cap"}
	}
	return ValidateValue(setting.Key, setting.Value, setting.Min, setting.Max)
}

// CheckUserOverride validates a user override against the application
// definition loaded for the write. found is false when that key has no
// application row. The key and the violated cap are named on failure.
func CheckUserOverride(key Key, setting Definition, found bool, raw json.RawMessage) error {
	if _, known := SchemaFor(key); !known {
		return &ValidationError{Key: key, Cause: "is not a registered setting key"}
	}
	if !found || setting.Key != key {
		return &ValidationError{Key: key, Cause: "is not configured"}
	}
	if !setting.UserOverridable {
		return &ValidationError{Key: key, Cause: "is not user-overridable"}
	}
	return ValidateValue(key, raw, setting.Min, setting.Max)
}

// ValidateValue checks a JSON value against its key's schema and optional
// application-level minimum and maximum caps. Application caps are reported
// before schema caps so the admin cap is the one named when both apply.
func ValidateValue(key Key, raw json.RawMessage, min, max *float64) error {
	schema, ok := schemas[key]
	if !ok {
		return &ValidationError{Key: key, Cause: "is not a registered setting key"}
	}
	value, err := decodeJSON(raw)
	if err != nil {
		return &ValidationError{Key: key, Cause: "must contain one valid JSON value"}
	}
	if schema.Type == "boolean" {
		if _, ok := value.(bool); !ok {
			return &ValidationError{Key: key, Cause: "must be a boolean (JSON schema type boolean)"}
		}
		if min != nil || max != nil {
			return &ValidationError{Key: key, Cause: "does not accept numeric caps"}
		}
		return nil
	}

	numericValue, err := schemaNumber(key, value, schema)
	if err != nil {
		return err
	}
	if cap, below, outside := outsideCap(numericValue, min, max); outside {
		return capError(key, cap, below, false)
	}
	if cap, below, outside := outsideCap(numericValue, schema.Minimum, schema.Maximum); outside {
		return capError(key, cap, below, true)
	}
	return nil
}

func schemaNumber(key Key, value any, schema JSONSchema) (float64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, &ValidationError{Key: key, Cause: fmt.Sprintf("must be a %s (JSON schema type %s)", schema.Type, schema.Type)}
	}
	if schema.Type == "integer" {
		if !integralToken(number) {
			return 0, &ValidationError{Key: key, Cause: "must be an integer (JSON schema type integer)"}
		}
		integer, err := number.Int64()
		if err != nil {
			return 0, &ValidationError{Key: key, Cause: "must be an integer within the signed 64-bit range (JSON schema type integer)"}
		}
		return float64(integer), nil
	}
	parsed, err := number.Float64()
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, &ValidationError{Key: key, Cause: fmt.Sprintf("must be a finite %s (JSON schema type %s)", schema.Type, schema.Type)}
	}
	return parsed, nil
}

func outsideCap(value float64, min, max *float64) (float64, bool, bool) {
	if min != nil && value < *min {
		return *min, true, true
	}
	if max != nil && value > *max {
		return *max, false, true
	}
	return 0, false, false
}

func capError(key Key, cap float64, below bool, schema bool) error {
	rendered := formatCap(cap)
	relation := "exceeds"
	if below {
		relation = "is below"
	}
	kind := "cap"
	if schema {
		kind = "schema cap"
	}
	return &ValidationError{Key: key, Cap: rendered, Cause: fmt.Sprintf("%s %s %s", relation, kind, rendered)}
}

func finiteCap(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0))
}

func integralToken(number json.Number) bool {
	text := string(number)
	if text == "" {
		return false
	}
	index := 0
	if text[0] == '-' {
		index = 1
	}
	if index == len(text) {
		return false
	}
	for ; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
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

func decodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

func integer(raw []byte) (int, error) {
	value, err := integer64(raw)
	if err != nil {
		return 0, err
	}
	converted := int(value)
	if int64(converted) != value {
		return 0, errors.New("integer overflows int")
	}
	return converted, nil
}

func integer64(raw []byte) (int64, error) {
	value, err := decodeJSON(raw)
	if err != nil {
		return 0, err
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, errors.New("not a JSON number")
	}
	return number.Int64()
}

func decimal(raw []byte) (float64, error) {
	value, err := decodeJSON(raw)
	if err != nil {
		return 0, err
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, errors.New("not a JSON number")
	}
	return number.Float64()
}

func duration(raw []byte) (time.Duration, error) {
	seconds, err := integer64(raw)
	if err != nil {
		return 0, err
	}
	if seconds > math.MaxInt64/int64(time.Second) || seconds < math.MinInt64/int64(time.Second) {
		return 0, errors.New("duration seconds overflow time.Duration")
	}
	return time.Duration(seconds) * time.Second, nil
}

func boolean(raw []byte) (bool, error) {
	value, err := decodeJSON(raw)
	if err != nil {
		return false, err
	}
	result, ok := value.(bool)
	if !ok {
		return false, errors.New("not a JSON boolean")
	}
	return result, nil
}

func validatedBoolean(raw []byte) bool {
	return string(bytes.TrimSpace(raw)) == "true"
}

func invalidStored(key Key, err error) error {
	return fmt.Errorf("settings: invalid stored value for %s: %w", key, err)
}

func formatCap(cap float64) string {
	return strconv.FormatFloat(cap, 'f', -1, 64)
}

func floatPointer(value float64) *float64 {
	return &value
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
