package app

import (
	"context"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

// SystemClock uses the wall clock.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// RetentionFunc adapts a function to domain.RetentionPolicy.
type RetentionFunc func(context.Context) (int, error)

// RetentionDays implements domain.RetentionPolicy.
func (f RetentionFunc) RetentionDays(ctx context.Context) (int, error) {
	return f(ctx)
}

var (
	_ domain.Clock           = SystemClock{}
	_ domain.RetentionPolicy = RetentionFunc(nil)
)
