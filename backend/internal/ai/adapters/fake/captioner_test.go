package fake

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jooservices/xflickr/internal/ai/app"
)

func TestCaptionerDerivesCaptionFromFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		text     string
	}{
		{name: "directory and extension", filename: "fixtures/cat-on-sofa.jpg", text: "Photo: cat-on-sofa"},
		{name: "multiple dots", filename: "fixtures/cat.on.sofa.tar.gz", text: "Photo: cat.on.sofa.tar"},
		{name: "no extension", filename: "cat-on-sofa", text: "Photo: cat-on-sofa"},
		{name: "backslash path", filename: `fixtures\cat-on-sofa.jpg`, text: "Photo: cat-on-sofa"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			captioner := mustNewCaptioner(t, test.filename)
			caption, err := captioner.Describe(t.Context(), []byte{1})
			if err != nil {
				t.Fatalf("Describe() error = %v", err)
			}
			want := app.Caption{
				Text:    test.text,
				Objects: []string{},
				Model:   "fake",
				Version: "1",
			}
			if !reflect.DeepEqual(caption, want) {
				t.Fatalf("Describe() = %#v, want %#v", caption, want)
			}
			if err := caption.Validate(); err != nil {
				t.Fatalf("Caption.Validate() error = %v", err)
			}
		})
	}
}

func TestCaptionerIsDeterministic(t *testing.T) {
	first := mustNewCaptioner(t, "fixtures/cat-on-sofa.jpg")
	second := mustNewCaptioner(t, "fixtures/cat-on-sofa.jpg")
	image := []byte{1, 2, 3}
	initial, err := first.Describe(t.Context(), image)
	if err != nil {
		t.Fatalf("first Describe() error = %v", err)
	}
	initial.Objects = []string{"caller mutation"}

	for range 3 {
		caption, err := first.Describe(t.Context(), image)
		if err != nil {
			t.Fatalf("Describe() error = %v", err)
		}
		other, err := second.Describe(t.Context(), image)
		if err != nil {
			t.Fatalf("second Describe() error = %v", err)
		}
		if !reflect.DeepEqual(caption, other) {
			t.Fatalf("Describe() results differ: %#v != %#v", caption, other)
		}
		if caption.Text != "Photo: cat-on-sofa" || len(caption.Objects) != 0 {
			t.Fatalf("Describe() after caller mutation = %#v", caption)
		}
	}
	different := mustNewCaptioner(t, "fixtures/dog-in-yard.jpg")
	caption, err := different.Describe(t.Context(), image)
	if err != nil {
		t.Fatalf("different Describe() error = %v", err)
	}
	if caption.Text != "Photo: dog-in-yard" {
		t.Fatalf("different filename caption = %q, want %q", caption.Text, "Photo: dog-in-yard")
	}
}

func TestNewCaptionerRejectsMissingStem(t *testing.T) {
	for _, filename := range []string{"", ".", "..", "/.jpg"} {
		t.Run(filename, func(t *testing.T) {
			if _, err := NewCaptioner(filename); !errors.Is(err, app.ErrInvalidConfiguration) {
				t.Fatalf("NewCaptioner(%q) error = %v, want ErrInvalidConfiguration", filename, err)
			}
		})
	}
}

func TestCaptionerRejectsInvalidInputAndCanceledContext(t *testing.T) {
	captioner := mustNewCaptioner(t, "fixtures/cat-on-sofa.jpg")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name  string
		ctx   context.Context
		image []byte
		want  error
	}{
		{name: "nil context", ctx: nil, image: []byte{1}, want: app.ErrInvalidConfiguration},
		{name: "nil context precedes empty image", ctx: nil, want: app.ErrInvalidConfiguration},
		{name: "nil image", ctx: t.Context(), want: app.ErrInvalidImage},
		{name: "empty image", ctx: t.Context(), image: []byte{}, want: app.ErrInvalidImage},
		{name: "empty image precedes canceled context", ctx: canceled, want: app.ErrInvalidImage},
		{name: "canceled context", ctx: canceled, image: []byte{1}, want: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := captioner.Describe(test.ctx, test.image)
			if !errors.Is(err, test.want) {
				t.Fatalf("Describe() error = %v, want error matching %v", err, test.want)
			}
		})
	}
}

func mustNewCaptioner(t *testing.T, filename string) *Captioner {
	t.Helper()
	captioner, err := NewCaptioner(filename)
	if err != nil {
		t.Fatalf("NewCaptioner(%q) error = %v", filename, err)
	}
	return captioner
}
