// Package app coordinates saved search use cases.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrInvalidSavedSearch indicates invalid saved search input.
	ErrInvalidSavedSearch = errors.New("search: invalid saved search")
	// ErrNotFound indicates a saved search does not belong to the requested user or does not exist.
	ErrNotFound = errors.New("search: saved search not found")
)

const maxSavedQueryRunes = 1000

// SavedSearch is one user's stored search query.
type SavedSearch struct {
	ID        int64
	UserID    int64
	Query     string
	CreatedAt time.Time
}

// SavedRepository persists saved searches for their owning user.
type SavedRepository interface {
	Create(context.Context, int64, string) (SavedSearch, error)
	Get(context.Context, int64, int64) (SavedSearch, error)
}

// SavedSearches coordinates saved search validation and persistence.
type SavedSearches struct {
	Repository SavedRepository
}

// Create validates and stores a trimmed query for userID.
func (service SavedSearches) Create(ctx context.Context, userID int64, query string) (SavedSearch, error) {
	query = strings.TrimSpace(query)
	if userID <= 0 || !validSavedQuery(query) {
		return SavedSearch{}, ErrInvalidSavedSearch
	}
	if service.Repository == nil {
		return SavedSearch{}, errors.New("search: saved search repository is required")
	}

	created, err := service.Repository.Create(ctx, userID, query)
	if err != nil {
		return SavedSearch{}, fmt.Errorf("search: create saved search: %w", err)
	}
	return created, nil
}

// Get reads one saved search only when it belongs to userID.
func (service SavedSearches) Get(ctx context.Context, userID, id int64) (SavedSearch, error) {
	if userID <= 0 || id <= 0 {
		return SavedSearch{}, ErrInvalidSavedSearch
	}
	if service.Repository == nil {
		return SavedSearch{}, errors.New("search: saved search repository is required")
	}

	search, err := service.Repository.Get(ctx, userID, id)
	if err != nil {
		return SavedSearch{}, fmt.Errorf("search: get saved search: %w", err)
	}
	return search, nil
}

func validSavedQuery(query string) bool {
	return query != "" && !strings.ContainsRune(query, '\x00') && utf8.RuneCountInString(query) <= maxSavedQueryRunes
}
