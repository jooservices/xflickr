// Package app defines ports for quota use cases and adapters.
package app

import (
	"context"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

// BudgetProvider resolves the current policy and limits for an API key.
type BudgetProvider interface {
	Budget(context.Context, int64) (domain.Budget, error)
}

// Governor is the application-facing quota permit port.
type Governor = domain.Governor
