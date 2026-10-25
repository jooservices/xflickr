// Package app defines ports for quota use cases and adapters.
package app

import (
	"context"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

// ErrKeyUnavailable means a key has no usable quota budget and should be skipped by dispatchers.
var ErrKeyUnavailable error = keyUnavailableError{}

type keyUnavailableError struct{}

func (keyUnavailableError) Error() string        { return "quota: key unavailable" }
func (keyUnavailableError) KeyUnavailable() bool { return true }

// BudgetProvider resolves the current policy and limits for an API key.
type BudgetProvider interface {
	Budget(context.Context, int64) (domain.Budget, error)
}

// Governor is the application-facing quota permit port.
type Governor = domain.Governor
