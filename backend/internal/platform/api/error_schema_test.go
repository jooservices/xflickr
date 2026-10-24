package api

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// TestErrorSchemaMapsToGoType checks that the shared OpenAPI Error schema
// (code, message, details) is generated as the Go type gen.Error.
func TestErrorSchemaMapsToGoType(t *testing.T) {
	typ := reflect.TypeOf(gen.Error{})
	if typ.Kind() != reflect.Struct || typ.Name() != "Error" {
		t.Fatalf("Error schema Go type = %s %s, want struct Error", typ.Kind(), typ.Name())
	}

	wantFields := map[string]struct {
		jsonTag string
		kind    reflect.Kind
	}{
		"Code":    {jsonTag: "code", kind: reflect.String},
		"Message": {jsonTag: "message", kind: reflect.String},
		"Details": {jsonTag: "details,omitempty", kind: reflect.Pointer},
	}
	if typ.NumField() != len(wantFields) {
		t.Fatalf("Error field count = %d, want %d", typ.NumField(), len(wantFields))
	}
	for name, want := range wantFields {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("Error is missing field %s", name)
		}
		if got := field.Tag.Get("json"); got != want.jsonTag {
			t.Errorf("Error.%s json tag = %q, want %q", name, got, want.jsonTag)
		}
		if field.Type.Kind() != want.kind {
			t.Errorf("Error.%s kind = %s, want %s", name, field.Type.Kind(), want.kind)
		}
	}

	detailsField, ok := typ.FieldByName("Details")
	if !ok {
		t.Fatal("Error is missing field Details")
	}
	detailsType := detailsField.Type
	if detailsType.Kind() != reflect.Pointer || detailsType.Elem().Kind() != reflect.Map || detailsType.Elem().Key().Kind() != reflect.String {
		t.Fatalf("Error.Details type = %s, want a pointer to map[string]...", detailsType)
	}

	const payload = `{"code":"not_found","message":"missing photo","details":{"id":"42","retryable":false}}`
	var body gen.Error
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatalf("unmarshal Error: %v", err)
	}
	if body.Code != "not_found" || body.Message != "missing photo" {
		t.Fatalf("Error identity = %+v", body)
	}
	if body.Details == nil {
		t.Fatal("Error.Details is nil")
	}
	if got := (*body.Details)["id"]; got != "42" {
		t.Errorf("details.id = %#v, want 42", got)
	}
	if got := (*body.Details)["retryable"]; got != false {
		t.Errorf("details.retryable = %#v, want false", got)
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal Error: %v", err)
	}
	for _, fragment := range []string{`"code":"not_found"`, `"message":"missing photo"`, `"details":`} {
		if !bytes.Contains(encoded, []byte(fragment)) {
			t.Errorf("encoded Error = %s, missing %s", encoded, fragment)
		}
	}

	var omitted gen.Error
	if err := json.Unmarshal([]byte(`{"code":"bad_request","message":"nope"}`), &omitted); err != nil {
		t.Fatalf("unmarshal Error without details: %v", err)
	}
	if omitted.Code != "bad_request" || omitted.Message != "nope" || omitted.Details != nil {
		t.Fatalf("Error without details = %+v", omitted)
	}
	encoded, err = json.Marshal(omitted)
	if err != nil {
		t.Fatalf("marshal Error without details: %v", err)
	}
	if bytes.Contains(encoded, []byte("details")) {
		t.Fatalf("encoded Error without details = %s", encoded)
	}

	var invalid gen.Error
	if err := json.Unmarshal([]byte(`{"code":"bad_request","message":"nope","details":"x"}`), &invalid); err == nil {
		t.Fatal("Error.Details accepted a string")
	}
}
