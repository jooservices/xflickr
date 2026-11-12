package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEveryEffectiveFieldHasOneDecoder(t *testing.T) {
	if got, want := len(effectiveFields), reflect.TypeFor[Effective]().NumField(); got != want {
		t.Fatalf("effectiveFields has %d entries, Effective has %d fields: a setting would be left at its zero value", got, want)
	}
}

func TestDecodedReportsAnInvalidStoredValue(t *testing.T) {
	var result Effective
	field := decoded(MaxDepth, integer, func(e *Effective) *int { return &e.MaxDepth })
	if err := field(map[Key]json.RawMessage{MaxDepth: json.RawMessage(`"not a number"`)}, &result); err == nil {
		t.Fatal("a value that cannot be decoded must be reported")
	}
	if err := field(map[Key]json.RawMessage{MaxDepth: json.RawMessage(`3`)}, &result); err != nil || result.MaxDepth != 3 {
		t.Fatalf("decoded = %d, err = %v; want 3", result.MaxDepth, err)
	}
}
