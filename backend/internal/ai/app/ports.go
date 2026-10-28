// Package app defines application types and ports for AI features.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidImage reports an empty image passed to a captioner.
	ErrInvalidImage = errors.New("ai: image is required")
	// ErrInvalidCaption reports a caption without required text or model metadata.
	ErrInvalidCaption = errors.New("ai: invalid caption")
	// ErrInvalidResponse reports a provider response that does not match the contract.
	ErrInvalidResponse = errors.New("ai: invalid provider response")
	// ErrInvalidConfiguration reports missing or unusable adapter settings.
	ErrInvalidConfiguration = errors.New("ai: invalid captioner configuration")
	// ErrCircuitOpen means the provider circuit breaker is preventing a request.
	ErrCircuitOpen = errors.New("ai: caption provider circuit is open")
)

// Caption is the structured description returned for an image.
type Caption struct {
	Text    string   `json:"text"`
	Objects []string `json:"objects"`
	Model   string   `json:"model"`
	Version string   `json:"version"`
}

// Validate checks the required description and model provenance.
func (c Caption) Validate() error {
	if strings.TrimSpace(c.Text) == "" || strings.TrimSpace(c.Model) == "" || strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("%w: text, model and version are required", ErrInvalidCaption)
	}
	return nil
}

// Captioner describes an image and identifies the model version used.
type Captioner interface {
	Describe(context.Context, []byte) (Caption, error)
}
