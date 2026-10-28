package app

import (
	"errors"
	"testing"
)

func TestCaptionValidate(t *testing.T) {
	tests := []struct {
		name    string
		caption Caption
		valid   bool
	}{
		{name: "complete", caption: Caption{Text: "A cat", Objects: []string{"cat"}, Model: "vision-model", Version: "2026-01"}, valid: true},
		{name: "empty objects are allowed", caption: Caption{Text: "A quiet room", Objects: []string{}, Model: "vision-model", Version: "2026-01"}, valid: true},
		{name: "missing text", caption: Caption{Model: "vision-model", Version: "2026-01"}},
		{name: "missing model", caption: Caption{Text: "A cat", Version: "2026-01"}},
		{name: "missing version", caption: Caption{Text: "A cat", Model: "vision-model"}},
		{name: "blank text", caption: Caption{Text: " \t ", Model: "vision-model", Version: "2026-01"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.caption.Validate()
			if test.valid && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidCaption) {
				t.Fatalf("Validate() error = %v, want ErrInvalidCaption", err)
			}
		})
	}
}
