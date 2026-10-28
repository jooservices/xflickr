package ollama

import (
	"errors"
	"testing"

	aiapp "github.com/jooservices/xflickr/internal/ai/app"
)

func TestParseCaption(t *testing.T) {
	tests := []struct {
		name    string
		content string
		valid   bool
	}{
		{name: "valid", content: `{"text":"A cat","objects":["cat"]}`, valid: true},
		{name: "empty objects", content: `{"text":"A room","objects":[]}`, valid: true},
		{name: "malformed JSON", content: `not-json`},
		{name: "missing objects", content: `{"text":"A cat"}`},
		{name: "null objects", content: `{"text":"A cat","objects":null}`},
		{name: "blank text", content: `{"text":"  ","objects":[]}`},
		{name: "blank object", content: `{"text":"A cat","objects":[" "]}`},
		{name: "unknown field", content: `{"text":"A cat","objects":[],"extra":true}`},
		{name: "trailing JSON value", content: `{"text":"A cat","objects":[]} {}`},
		{name: "malformed trailer", content: `{"text":"A cat","objects":[]} trailing`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caption, err := parseCaption(test.content, "vision-model", "modified-at")
			if test.valid {
				if err != nil {
					t.Fatalf("parseCaption() error = %v", err)
				}
				if err := caption.Validate(); err != nil || caption.Model != "vision-model" || caption.Version != "modified-at" {
					t.Fatalf("parsed caption = %#v, validation error = %v", caption, err)
				}
				return
			}
			if !errors.Is(err, aiapp.ErrInvalidResponse) {
				t.Fatalf("parseCaption() error = %v, want ErrInvalidResponse", err)
			}
		})
	}
}
