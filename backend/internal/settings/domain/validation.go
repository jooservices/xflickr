package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"
)

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
