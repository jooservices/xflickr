// Package postgres persists saved searches in PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/search/app"
)

// Repository stores and reads user-owned saved searches.
type Repository struct {
	pool     *pgxpool.Pool
	queryRow func(context.Context, string, ...any) pgx.Row
}

var _ app.SavedRepository = (*Repository)(nil)

// NewSavedRepository creates a saved search repository backed by pool.
func NewSavedRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("search: PostgreSQL pool is required")
	}
	return &Repository{pool: pool}, nil
}

// NewSavedRepositoryWithQueryRow creates a repository using a pgx-compatible query function.
func NewSavedRepositoryWithQueryRow(queryRow func(context.Context, string, ...any) pgx.Row) (*Repository, error) {
	if queryRow == nil {
		return nil, errors.New("search: PostgreSQL query function is required")
	}
	return &Repository{queryRow: queryRow}, nil
}

// Create inserts a saved search and returns its database-assigned values.
func (repository *Repository) Create(ctx context.Context, userID int64, query string) (app.SavedSearch, error) {
	var saved app.SavedSearch
	err := repository.query(ctx, `
INSERT INTO saved_searches (user_id, query)
VALUES ($1, $2)
RETURNING id, user_id, query, created_at`, userID, query).Scan(
		&saved.ID, &saved.UserID, &saved.Query, &saved.CreatedAt,
	)
	if err != nil {
		return app.SavedSearch{}, fmt.Errorf("search: create saved search: %w", err)
	}
	return saved, nil
}

// Get reads a saved search owned by userID.
func (repository *Repository) Get(ctx context.Context, userID, id int64) (app.SavedSearch, error) {
	var saved app.SavedSearch
	err := repository.query(ctx, `
SELECT id, user_id, query, created_at
FROM saved_searches
WHERE user_id = $1 AND id = $2`, userID, id).Scan(
		&saved.ID, &saved.UserID, &saved.Query, &saved.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.SavedSearch{}, app.ErrNotFound
		}
		return app.SavedSearch{}, fmt.Errorf("search: get saved search: %w", err)
	}
	return saved, nil
}

func (repository *Repository) query(ctx context.Context, statement string, args ...any) pgx.Row {
	if repository.queryRow != nil {
		return repository.queryRow(ctx, statement, args...)
	}
	return repository.pool.QueryRow(ctx, statement, args...)
}
