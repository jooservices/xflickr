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
	MaxDepth            Key = "max_depth"
	ChangeCheckInterval Key = "change_check_interval"
	DailyCallLimit      Key = "daily_call_limit"
	QuotaReserve        Key = "quota_reserve"
	PerSecondRate       Key = "per_second_rate"
	Burst               Key = "burst"
	PersonLaneShare     Key = "person_lane_share"
	ZipTTL              Key = "zip_ttl"
	RetryMax            Key = "retry_max"
	RetryBackoff        Key = "retry_backoff"
	NodeCooldown        Key = "node_cooldown"
	GroupPoolsEnabled   Key = "group_pools_enabled"
	RegistrationOpen    Key = "registration_open"
)

// Effective contains the merged, validated settings for one user. Durations
// are stored in JSON as integer seconds and converted to time.Duration here.
type Effective struct {
	MaxDepth            int
	ChangeCheckInterval time.Duration
	DailyCallLimit      int64 // zero means unlimited
	QuotaReserve        int64
	PerSecondRate       int
	Burst               int
	PersonLaneShare     float64
	ZipTTL              time.Duration
	RetryMax            int
	RetryBackoff        time.Duration
	NodeCooldown        time.Duration
	GroupPoolsEnabled   bool
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
	MaxDepth:            {Type: "integer", Minimum: minOne},
	ChangeCheckInterval: {Type: "integer", Minimum: minOne, Maximum: maxDuration},
	DailyCallLimit:      {Type: "integer", Minimum: minZero},
	QuotaReserve:        {Type: "integer", Minimum: minZero},
	PerSecondRate:       {Type: "integer", Minimum: minOne},
	Burst:               {Type: "integer", Minimum: minOne},
	PersonLaneShare:     {Type: "number", Minimum: minZero, Maximum: maxOne},
	ZipTTL:              {Type: "integer", Minimum: minOne, Maximum: maxDuration},
	RetryMax:            {Type: "integer", Minimum: minZero},
	RetryBackoff:        {Type: "integer", Minimum: minZero, Maximum: maxDuration},
	NodeCooldown:        {Type: "integer", Minimum: minZero, Maximum: maxDuration},
	GroupPoolsEnabled:   {Type: "boolean"},
	RegistrationOpen:    {Type: "boolean"},
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
		DailyCallLimit,
		QuotaReserve,
		PerSecondRate,
		Burst,
		PersonLaneShare,
		ZipTTL,
		RetryMax,
		RetryBackoff,
		NodeCooldown,
		GroupPoolsEnabled,
		RegistrationOpen,
	}
}

// Defaults returns the seeded defaults and admin caps as domain values.
func Defaults() []Definition {
	return []Definition{
		{Key: MaxDepth, Value: json.RawMessage("2"), UserOverridable: true, Min: floatPointer(1), Max: floatPointer(4)},
		{Key: ChangeCheckInterval, Value: json.RawMessage("86400"), UserOverridable: true},
		{Key: DailyCallLimit, Value: json.RawMessage("0"), UserOverridable: true},
		{Key: QuotaReserve, Value: json.RawMessage("100"), UserOverridable: true},
		{Key: PerSecondRate, Value: json.RawMessage("4"), UserOverridable: true},
		{Key: Burst, Value: json.RawMessage("5"), UserOverridable: true},
		{Key: PersonLaneShare, Value: json.RawMessage("0.30"), UserOverridable: true},
		{Key: ZipTTL, Value: json.RawMessage("86400"), UserOverridable: true},
		{Key: RetryMax, Value: json.RawMessage("5"), UserOverridable: true},
		{Key: RetryBackoff, Value: json.RawMessage("30"), UserOverridable: true},
		{Key: NodeCooldown, Value: json.RawMessage("2700"), UserOverridable: true},
		{Key: GroupPoolsEnabled, Value: json.RawMessage("false"), UserOverridable: true},
		{Key: RegistrationOpen, Value: json.RawMessage("false"), UserOverridable: false},
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
func Merge(snapshot Snapshot) (Effective, []DroppedOverride, error) {
	if len(snapshot.Settings) != len(schemas) {
		return Effective{}, nil, fmt.Errorf("settings: got %d application settings, want %d", len(snapshot.Settings), len(schemas))
	}
	for key, setting := range snapshot.Settings {
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

	values := make(map[Key]json.RawMessage, len(snapshot.Settings))
	for key, setting := range snapshot.Settings {
		values[key] = setting.Value
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
	if result.DailyCallLimit, err = integer64(values[DailyCallLimit]); err != nil {
		return Effective{}, nil, invalidStored(DailyCallLimit, err)
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
	if result.GroupPoolsEnabled, err = boolean(values[GroupPoolsEnabled]); err != nil {
		return Effective{}, nil, invalidStored(GroupPoolsEnabled, err)
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

func invalidStored(key Key, err error) error {
	return fmt.Errorf("settings: invalid stored value for %s: %w", key, err)
}

func formatCap(cap float64) string {
	return strconv.FormatFloat(cap, 'f', -1, 64)
}

func floatPointer(value float64) *float64 {
	return &value
}
