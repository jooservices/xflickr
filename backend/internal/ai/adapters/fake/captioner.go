// Package fake provides deterministic AI adapters for tests.
package fake

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/jooservices/xflickr/internal/ai/app"
)

// Captioner returns a deterministic caption derived from a filename.
type Captioner struct {
	text string
}

// NewCaptioner creates a fake captioner using the filename stem as its hint.
func NewCaptioner(filename string) (*Captioner, error) {
	filename = strings.ReplaceAll(filename, `\`, "/")
	base := path.Base(filename)
	stem := strings.TrimSpace(strings.TrimSuffix(base, path.Ext(base)))
	if stem == "" || stem == "." || stem == ".." {
		return nil, fmt.Errorf("fake ai captioner: %w: filename stem is required", app.ErrInvalidConfiguration)
	}

	return &Captioner{text: "Photo: " + stem}, nil
}

// Describe returns the filename hint without inspecting the image or using a provider.
func (c *Captioner) Describe(ctx context.Context, image []byte) (app.Caption, error) {
	if ctx == nil {
		return app.Caption{}, fmt.Errorf("fake ai captioner: %w: context is required", app.ErrInvalidConfiguration)
	}
	if len(image) == 0 {
		return app.Caption{}, app.ErrInvalidImage
	}
	if err := ctx.Err(); err != nil {
		return app.Caption{}, err
	}

	return app.Caption{
		Text:    c.text,
		Objects: []string{},
		Model:   "fake",
		Version: "1",
	}, nil
}

var _ app.Captioner = (*Captioner)(nil)
