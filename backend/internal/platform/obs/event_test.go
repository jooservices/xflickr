package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestEventEmitsWithoutSpanAndIgnoresBlankNames(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(NewHandler(&output, slog.LevelDebug, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	Event(nil, "test.event", nil)
	Event(context.Background(), "  ", map[string]any{"should_not_log": true})
	got := output.String()
	if strings.Count(got, "test.event") != 2 || strings.Contains(got, "should_not_log") {
		t.Fatalf("unexpected event output: %s", got)
	}
	if empty := redactAnyMap(nil); len(empty) != 0 {
		t.Fatalf("redactAnyMap(nil) = %#v", empty)
	}
}

func TestToAttributePreservesSupportedOTelTypes(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		wantType attribute.Type
	}{
		{name: "string", value: "text", wantType: attribute.STRING},
		{name: "bool", value: true, wantType: attribute.BOOL},
		{name: "int", value: 7, wantType: attribute.INT64},
		{name: "int64", value: int64(8), wantType: attribute.INT64},
		{name: "uint64", value: uint64(9), wantType: attribute.INT64},
		{name: "uint64-overflow", value: ^uint64(0), wantType: attribute.STRING},
		{name: "float64", value: 1.5, wantType: attribute.FLOAT64},
		{name: "strings", value: []string{"a"}, wantType: attribute.STRINGSLICE},
		{name: "ints", value: []int64{1}, wantType: attribute.INT64SLICE},
		{name: "bools", value: []bool{true}, wantType: attribute.BOOLSLICE},
		{name: "structured", value: map[string]any{"safe": "yes"}, wantType: attribute.STRING},
		{name: "unmarshalable", value: make(chan int), wantType: attribute.STRING},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := toAttribute("field", test.value).Value
			if got.Type() != test.wantType {
				t.Fatalf("toAttribute() type = %v, want %v", got, test.wantType)
			}
			if test.name == "uint64-overflow" && got.AsString() != "18446744073709551615" {
				t.Fatal("overflowing uint64 attribute was not preserved exactly")
			}
		})
	}
	encoded, err := json.Marshal(toAttribute("field", map[string]any{"safe": "yes"}).Value.AsInterface())
	if err != nil || !strings.Contains(string(encoded), "safe") {
		t.Fatalf("structured attribute = %s (error %v)", encoded, err)
	}
}
