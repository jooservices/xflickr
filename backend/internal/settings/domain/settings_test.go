package domain

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMergeSeededDefaults(t *testing.T) {
	for _, setting := range Defaults() {
		if err := ValidateDefinition(setting); err != nil {
			t.Errorf("default %s: %v", setting.Key, err)
		}
	}
	depth := definition(t, MaxDepth)
	if string(depth.Value) != "2" || !depth.UserOverridable || !floatEqual(depth.Min, 1) || !floatEqual(depth.Max, 4) {
		t.Fatalf("max_depth default = %+v", depth)
	}

	got, dropped, err := Merge(DefaultSnapshot())
	if err != nil {
		t.Fatalf("Merge(defaults) error = %v", err)
	}
	if len(dropped) != 0 {
		t.Fatalf("dropped defaults = %+v", dropped)
	}
	want := Effective{
		MaxDepth:                2,
		ChangeCheckInterval:     24 * time.Hour,
		ReconcileInterval:       30 * 24 * time.Hour,
		DailyCallLimit:          0,
		QuotaReserve:            100,
		PerSecondRate:           4,
		Burst:                   5,
		PersonLaneShare:         0.30,
		ZipTTL:                  24 * time.Hour,
		RetryMax:                5,
		RetryBackoff:            30 * time.Second,
		NodeCooldown:            45 * time.Minute,
		GroupPoolsEnabled:       false,
		PhotoInfoEnabled:        true,
		PhotoExifEnabled:        true,
		PhotoContextsEnabled:    true,
		PhotoCommentsEnabled:    false,
		PhotoFavoritesEnabled:   false,
		PerKeyReadyCap:          100,
		NodeReadyCap:            500,
		MediaReadyCap:           100,
		AIReadyCap:              100,
		SyncReadyCap:            100,
		LeaseTimeout:            2 * time.Minute,
		ScoreWeightPlan:         10,
		ScoreWeightDirectAction: 100,
		ScoreWeightStarred:      20,
		ScoreWeightDepth:        5,
		ScoreWeightAgeMinutes:   0.1,
		ScoreAgingBatchSize:     500,
	}
	if got != want {
		t.Fatalf("effective defaults = %+v, want %+v", got, want)
	}
}

func TestJSONSchemaPerKey(t *testing.T) {
	documents := map[Key]string{
		MaxDepth:              `{"type":"integer","minimum":1}`,
		DailyCallLimit:        `{"type":"integer","minimum":0}`,
		QuotaReserve:          `{"type":"integer","minimum":0}`,
		PerSecondRate:         `{"type":"integer","minimum":1}`,
		Burst:                 `{"type":"integer","minimum":1}`,
		PersonLaneShare:       `{"type":"number","minimum":0,"maximum":1}`,
		RetryMax:              `{"type":"integer","minimum":0}`,
		GroupPoolsEnabled:     `{"type":"boolean"}`,
		PhotoInfoEnabled:      `{"type":"boolean"}`,
		PhotoExifEnabled:      `{"type":"boolean"}`,
		PhotoContextsEnabled:  `{"type":"boolean"}`,
		PhotoCommentsEnabled:  `{"type":"boolean"}`,
		PhotoFavoritesEnabled: `{"type":"boolean"}`,
	}
	if len(Keys()) != len(schemas) {
		t.Fatalf("keys = %d, schemas = %d", len(Keys()), len(schemas))
	}
	seen := map[Key]bool{}
	for _, key := range Keys() {
		seen[key] = true
		schema, ok := SchemaFor(key)
		if !ok {
			t.Fatalf("SchemaFor(%s) missing", key)
		}
		encoded, err := json.Marshal(schema)
		if err != nil {
			t.Fatalf("marshal schema %s: %v", key, err)
		}
		if want, ok := documents[key]; ok && string(encoded) != want {
			t.Fatalf("schema %s = %s, want %s", key, encoded, want)
		}
	}
	for key := range schemas {
		if !seen[key] {
			t.Fatalf("schema %s is not returned by Keys", key)
		}
	}
	if _, ok := SchemaFor("missing"); ok {
		t.Fatal("SchemaFor(missing) = true")
	}

	for _, key := range []Key{ChangeCheckInterval, ReconcileInterval, ZipTTL, RetryBackoff, NodeCooldown} {
		schema, ok := SchemaFor(key)
		if !ok || schema.Type != "integer" || !floatEqual(schema.Minimum, 0) && !floatEqual(schema.Minimum, 1) || !floatEqual(schema.Maximum, float64(math.MaxInt64/int64(time.Second))) {
			t.Fatalf("duration schema %s = %+v", key, schema)
		}
		if key != RetryBackoff && key != NodeCooldown && !floatEqual(schema.Minimum, 1) {
			t.Fatalf("%s minimum = %v, want 1", key, schema.Minimum)
		}
	}
}

func TestValidateValueAcceptsValuesInsideCaps(t *testing.T) {
	minimum := 3600.0
	tests := []struct {
		name string
		key  Key
		raw  string
		min  *float64
		max  *float64
	}{
		{name: "depth at admin minimum", key: MaxDepth, raw: "1"},
		{name: "depth at admin maximum", key: MaxDepth, raw: "4"},
		{name: "depth with surrounding space", key: MaxDepth, raw: " 2 "},
		{name: "unlimited daily calls", key: DailyCallLimit, raw: "0"},
		{name: "lane share boundaries", key: PersonLaneShare, raw: "0.30"},
		{name: "lane share one", key: PersonLaneShare, raw: "1"},
		{name: "group pools enabled", key: GroupPoolsEnabled, raw: "true"},
		{name: "group pools disabled", key: GroupPoolsEnabled, raw: "false"},
		{name: "interval at tighter admin cap", key: ChangeCheckInterval, raw: "3600", min: &minimum},
		{name: "zero retry backoff", key: RetryBackoff, raw: "0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			min, max := test.min, test.max
			if test.key == MaxDepth && min == nil && max == nil {
				min, max = floatPointer(1), floatPointer(4)
			}
			if err := ValidateValue(test.key, json.RawMessage(test.raw), min, max); err != nil {
				t.Fatalf("ValidateValue() error = %v", err)
			}
		})
	}
}

func TestValidateValueRejectsValuesOutsideCaps(t *testing.T) {
	tests := []struct {
		name    string
		key     Key
		raw     string
		min     *float64
		max     *float64
		wantCap string
		want    string
	}{
		{name: "depth above admin cap", key: MaxDepth, raw: "5", min: floatPointer(1), max: floatPointer(4), wantCap: "4", want: "exceeds cap 4"},
		{name: "depth below admin cap", key: MaxDepth, raw: "0", min: floatPointer(1), max: floatPointer(4), wantCap: "1", want: "is below cap 1"},
		{name: "depth below schema cap", key: MaxDepth, raw: "0", wantCap: "1", want: "schema cap 1"},
		{name: "negative daily limit", key: DailyCallLimit, raw: "-1", wantCap: "0", want: "schema cap 0"},
		{name: "lane share above one", key: PersonLaneShare, raw: "1.01", wantCap: "1", want: "schema cap 1"},
		{name: "lane share below zero", key: PersonLaneShare, raw: "-0.01", wantCap: "0", want: "schema cap 0"},
		{name: "interval below admin cap", key: ChangeCheckInterval, raw: "60", min: floatPointer(3600), wantCap: "3600", want: "is below cap 3600"},
		{name: "lane share above fractional cap", key: PersonLaneShare, raw: "0.3", max: floatPointer(0.25), wantCap: "0.25", want: "exceeds cap 0.25"},
		{name: "lane share below fractional cap", key: PersonLaneShare, raw: "0.1", min: floatPointer(0.25), wantCap: "0.25", want: "is below cap 0.25"},
		{name: "interval above duration schema", key: ChangeCheckInterval, raw: "9223372037", wantCap: "9223372036", want: "schema cap"},
		{name: "fractional integer", key: Burst, raw: "1.5", want: "must be an integer"},
		{name: "integer with fraction zero", key: Burst, raw: "1.0", want: "must be an integer"},
		{name: "integer exponent", key: Burst, raw: "1e1", want: "must be an integer"},
		{name: "integer overflow", key: DailyCallLimit, raw: "99999999999999999999", want: "signed 64-bit range"},
		{name: "boolean string", key: GroupPoolsEnabled, raw: `"true"`, want: "must be a boolean"},
		{name: "number for boolean", key: GroupPoolsEnabled, raw: "1", want: "must be a boolean"},
		{name: "boolean for integer", key: MaxDepth, raw: "true", want: "must be a integer"},
		{name: "string number", key: MaxDepth, raw: `"2"`, want: "must be a integer"},
		{name: "null", key: MaxDepth, raw: "null", want: "must be a integer"},
		{name: "array", key: MaxDepth, raw: "[2]", want: "must be a integer"},
		{name: "empty", key: MaxDepth, raw: "", want: "one valid JSON value"},
		{name: "two values", key: MaxDepth, raw: "2 2", want: "one valid JSON value"},
		{name: "unknown key", key: "nope", raw: "1", want: "not a registered setting key"},
		{name: "non-finite number", key: PersonLaneShare, raw: "1e309", want: "finite number"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateValue(test.key, json.RawMessage(test.raw), test.min, test.max)
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("ValidateValue() error = %v, want ValidationError", err)
			}
			if validation.Key != test.key {
				t.Fatalf("key = %s, want %s", validation.Key, test.key)
			}
			if test.wantCap != "" && validation.Cap != test.wantCap {
				t.Fatalf("cap = %q, want %q (%v)", validation.Cap, test.wantCap, err)
			}
			if !strings.Contains(err.Error(), string(test.key)) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want key %s and %q", err, test.key, test.want)
			}
		})
	}
}

func TestValidateDefinitionRejectsInvalidCaps(t *testing.T) {
	nan := math.NaN()
	positiveInf := math.Inf(1)
	tests := []struct {
		name    string
		setting Definition
		want    string
	}{
		{
			name:    "minimum above maximum",
			setting: Definition{Key: MaxDepth, Value: json.RawMessage("2"), Min: floatPointer(5), Max: floatPointer(1)},
			want:    "minimum cap exceeds maximum cap",
		},
		{
			name:    "non-finite minimum",
			setting: Definition{Key: MaxDepth, Value: json.RawMessage("2"), Min: &nan},
			want:    "caps must be finite",
		},
		{
			name:    "non-finite maximum",
			setting: Definition{Key: Burst, Value: json.RawMessage("1"), Max: &positiveInf},
			want:    "caps must be finite",
		},
		{
			name:    "boolean caps",
			setting: Definition{Key: GroupPoolsEnabled, Value: json.RawMessage("false"), Max: floatPointer(1)},
			want:    "does not accept numeric caps",
		},
		{
			name:    "value outside its own cap",
			setting: Definition{Key: MaxDepth, Value: json.RawMessage("5"), Min: floatPointer(1), Max: floatPointer(4), UserOverridable: true},
			want:    "exceeds cap 4",
		},
		{
			name:    "unknown key",
			setting: Definition{Key: "custom", Value: json.RawMessage("1")},
			want:    "not a registered setting key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDefinition(test.setting)
			if err == nil || !strings.Contains(err.Error(), string(test.setting.Key)) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateDefinition() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMergeAppliesOverridesInsideCaps(t *testing.T) {
	snapshot := DefaultSnapshot()
	snapshot.Overrides = map[Key]json.RawMessage{
		MaxDepth:          json.RawMessage("4"),
		GroupPoolsEnabled: json.RawMessage("true"),
		PersonLaneShare:   json.RawMessage("0"),
	}
	got, dropped, err := Merge(snapshot)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if len(dropped) != 0 {
		t.Fatalf("dropped = %+v", dropped)
	}
	if got.MaxDepth != 4 || !got.GroupPoolsEnabled || got.PersonLaneShare != 0 || got.QuotaReserve != 100 || got.ChangeCheckInterval != 24*time.Hour {
		t.Fatalf("merged = %+v", got)
	}
}

func TestMergeKeepsPhotoFacetSettingsApplicationOnly(t *testing.T) {
	snapshot := DefaultSnapshot()
	snapshot.Overrides = map[Key]json.RawMessage{
		PhotoInfoEnabled:      json.RawMessage("false"),
		PhotoExifEnabled:      json.RawMessage("false"),
		PhotoContextsEnabled:  json.RawMessage("false"),
		PhotoCommentsEnabled:  json.RawMessage("true"),
		PhotoFavoritesEnabled: json.RawMessage("true"),
	}
	got, dropped, err := Merge(snapshot)
	if err != nil {
		t.Fatalf("Merge(photo facet overrides) error = %v", err)
	}
	if got.PhotoInfoEnabled != true || got.PhotoExifEnabled != true || got.PhotoContextsEnabled != true || got.PhotoCommentsEnabled || got.PhotoFavoritesEnabled {
		t.Fatalf("photo facet effective settings = %+v, want application defaults", got)
	}
	want := []Key{PhotoCommentsEnabled, PhotoContextsEnabled, PhotoExifEnabled, PhotoFavoritesEnabled, PhotoInfoEnabled}
	if len(dropped) != len(want) {
		t.Fatalf("dropped photo facet overrides = %+v, want %d", dropped, len(want))
	}
	for index, key := range want {
		if dropped[index].Key != key || dropped[index].Reason != "is not user-overridable" {
			t.Errorf("dropped[%d] = %+v, want %s app-only override", index, dropped[index], key)
		}
	}
}

func TestCheckUserOverride(t *testing.T) {
	depth := definition(t, MaxDepth)
	if err := CheckUserOverride(MaxDepth, depth, true, json.RawMessage("4")); err != nil {
		t.Fatalf("CheckUserOverride(inside cap) error = %v", err)
	}

	tests := []struct {
		name    string
		key     Key
		setting Definition
		found   bool
		raw     string
		want    string
		wantCap string
	}{
		{name: "unknown key", key: "nope", raw: "1", want: "not a registered setting key"},
		{name: "missing row", key: MaxDepth, raw: "2", want: "is not configured"},
		{name: "row key mismatch", key: Burst, setting: depth, found: true, raw: "2", want: "is not configured"},
		{name: "not overridable", key: MaxDepth, setting: Definition{Key: MaxDepth, UserOverridable: false, Value: json.RawMessage("2"), Min: floatPointer(1), Max: floatPointer(4)}, found: true, raw: "3", want: "not user-overridable"},
		{name: "above cap", key: MaxDepth, setting: depth, found: true, raw: "5", want: "exceeds cap 4", wantCap: "4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CheckUserOverride(test.key, test.setting, test.found, json.RawMessage(test.raw))
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Key != test.key || validation.Cap != test.wantCap || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckUserOverride() error = %v, want key %s cap %q and %q", err, test.key, test.wantCap, test.want)
			}
		})
	}
}

func TestMergeDropsStaleOverrides(t *testing.T) {
	t.Run("cap tightened", func(t *testing.T) {
		snapshot := DefaultSnapshot()
		setting := snapshot.Settings[MaxDepth]
		maximum := 3.0
		setting.Max = &maximum
		snapshot.Settings[MaxDepth] = setting
		snapshot.Overrides = map[Key]json.RawMessage{
			MaxDepth: json.RawMessage("4"),
			Burst:    json.RawMessage("3"),
		}
		got, dropped, err := Merge(snapshot)
		if err != nil {
			t.Fatalf("Merge() error = %v", err)
		}
		if got.MaxDepth != 2 || got.Burst != 3 {
			t.Fatalf("merged = %+v, want default depth and the burst override", got)
		}
		if len(dropped) != 1 || dropped[0].Key != MaxDepth || !strings.Contains(dropped[0].Reason, "exceeds cap 3") {
			t.Fatalf("dropped = %+v", dropped)
		}
	})

	t.Run("overridable flipped", func(t *testing.T) {
		snapshot := DefaultSnapshot()
		setting := snapshot.Settings[GroupPoolsEnabled]
		setting.UserOverridable = false
		snapshot.Settings[GroupPoolsEnabled] = setting
		snapshot.Overrides = map[Key]json.RawMessage{GroupPoolsEnabled: json.RawMessage("true")}
		got, dropped, err := Merge(snapshot)
		if err != nil {
			t.Fatalf("Merge() error = %v", err)
		}
		if got.GroupPoolsEnabled {
			t.Fatalf("merged = %+v, want the application default", got)
		}
		if len(dropped) != 1 || dropped[0].Key != GroupPoolsEnabled || dropped[0].Reason != "is not user-overridable" {
			t.Fatalf("dropped = %+v", dropped)
		}
	})

	t.Run("reports every stale override in key order", func(t *testing.T) {
		snapshot := DefaultSnapshot()
		depth := snapshot.Settings[MaxDepth]
		maximum := 3.0
		depth.Max = &maximum
		snapshot.Settings[MaxDepth] = depth
		pools := snapshot.Settings[GroupPoolsEnabled]
		pools.UserOverridable = false
		snapshot.Settings[GroupPoolsEnabled] = pools
		snapshot.Overrides = map[Key]json.RawMessage{
			MaxDepth:          json.RawMessage("4"),
			GroupPoolsEnabled: json.RawMessage("true"),
		}
		_, dropped, err := Merge(snapshot)
		if err != nil {
			t.Fatalf("Merge() error = %v", err)
		}
		if len(dropped) != 2 || dropped[0].Key != GroupPoolsEnabled || dropped[1].Key != MaxDepth {
			t.Fatalf("dropped = %+v", dropped)
		}
	})
}

func TestMergeRejectsInvalidOverridesAndSnapshots(t *testing.T) {
	t.Run("unknown override", func(t *testing.T) {
		snapshot := DefaultSnapshot()
		snapshot.Overrides = map[Key]json.RawMessage{"extra": json.RawMessage("1")}
		_, _, err := Merge(snapshot)
		if err == nil || !strings.Contains(err.Error(), "extra") || !strings.Contains(err.Error(), "not a registered setting key") {
			t.Fatalf("Merge() error = %v", err)
		}
	})

	t.Run("missing settings", func(t *testing.T) {
		_, _, err := Merge(Snapshot{})
		if err == nil || !strings.Contains(err.Error(), "got 0 application settings") {
			t.Fatalf("Merge() error = %v", err)
		}
	})

	t.Run("map key mismatch", func(t *testing.T) {
		snapshot := DefaultSnapshot()
		setting := snapshot.Settings[MaxDepth]
		setting.Key = Burst
		snapshot.Settings[MaxDepth] = setting
		_, _, err := Merge(snapshot)
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("Merge() error = %v", err)
		}
	})

	t.Run("invalid stored default", func(t *testing.T) {
		snapshot := DefaultSnapshot()
		setting := snapshot.Settings[MaxDepth]
		setting.Value = json.RawMessage("true")
		snapshot.Settings[MaxDepth] = setting
		_, _, err := Merge(snapshot)
		if err == nil || !strings.Contains(err.Error(), "max_depth") {
			t.Fatalf("Merge() error = %v", err)
		}
	})
}

func TestValidationErrorFormats(t *testing.T) {
	if got := (*ValidationError)(nil).Error(); got != "settings: validation failed" {
		t.Fatalf("nil error = %q", got)
	}
	if got := (&ValidationError{}).Error(); got != "settings: validation failed" {
		t.Fatalf("empty error = %q", got)
	}
	if got := (&ValidationError{Key: MaxDepth, Cap: "4"}).Error(); !strings.Contains(got, "max_depth") || !strings.Contains(got, "4") {
		t.Fatalf("cap error = %q", got)
	}
	if got := (&ValidationError{Key: MaxDepth, Cause: "is not user-overridable"}).Error(); got != "settings: max_depth is not user-overridable" {
		t.Fatalf("cause error = %q", got)
	}
}

func TestStoredValueParsers(t *testing.T) {
	if _, err := duration([]byte(strconv.FormatInt(math.MaxInt64, 10))); err == nil {
		t.Fatal("duration() accepted a value that overflows time.Duration")
	}
	if _, err := duration([]byte("-9223372037")); err == nil {
		t.Fatal("duration() accepted a value below time.Duration")
	}
	if _, err := integer([]byte("1 {")); err == nil {
		t.Fatal("integer() accepted trailing junk")
	}
	got, err := duration([]byte("30"))
	if err != nil || got != 30*time.Second {
		t.Fatalf("duration(30) = %s, %v", got, err)
	}
	if _, err := integer([]byte("1.5")); err == nil {
		t.Fatal("integer() accepted a fraction")
	}
	if _, err := integer64([]byte("true")); err == nil {
		t.Fatal("integer64() accepted a boolean")
	}
	if _, err := boolean([]byte("1")); err == nil {
		t.Fatal("boolean() accepted a number")
	}
	if _, err := decimal([]byte("true")); err == nil {
		t.Fatal("decimal() accepted a boolean")
	}
	if got := dropReason(&ValidationError{Key: MaxDepth, Cap: "4"}); !strings.Contains(got, "max_depth") || !strings.Contains(got, "4") {
		t.Fatalf("dropReason() = %q", got)
	}
	if _, err := decodeJSON([]byte("1 2")); err == nil {
		t.Fatal("decodeJSON() accepted two values")
	}
	if err := invalidStored(MaxDepth, errors.New("boom")); !strings.Contains(err.Error(), "max_depth") || !errors.Is(err, errors.New("boom")) && !strings.Contains(err.Error(), "boom") {
		t.Fatalf("invalidStored() = %v", err)
	}
}

func definition(t *testing.T, key Key) Definition {
	t.Helper()
	for _, setting := range Defaults() {
		if setting.Key == key {
			return setting
		}
	}
	t.Fatalf("default %s is missing", key)
	return Definition{}
}

func floatEqual(value *float64, want float64) bool {
	return value != nil && *value == want
}
