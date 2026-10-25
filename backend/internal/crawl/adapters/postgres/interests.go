// Package postgres stores per-user crawl interests.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
)

var (
	errDatabaseRequired = errors.New("crawl: postgres database is required")
	errContextRequired  = errors.New("crawl: context is required")
)

type rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (rows, error)
}

type poolAdapter struct{ pool *pgxpool.Pool }

func (pool poolAdapter) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return pool.pool.Exec(ctx, query, args...)
}

func (pool poolAdapter) Query(ctx context.Context, query string, args ...any) (rows, error) {
	result, err := pool.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Repository persists crawl interests.
type Repository struct{ pool database }

// New creates a repository backed by PostgreSQL.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errDatabaseRequired
	}
	return &Repository{pool: poolAdapter{pool: pool}}, nil
}

// Add inserts an interest or keeps the shallower depth already stored for the
// same user and person. Via follows the path that provided that shallowest depth.
func (repository *Repository) Add(ctx context.Context, interest crawldomain.Interest) error {
	if repository == nil || repository.pool == nil {
		return errDatabaseRequired
	}
	if ctx == nil {
		return errContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := interest.Validate(); err != nil {
		return err
	}
	_, err := repository.pool.Exec(ctx, `
		INSERT INTO crawl_interests (user_id, person_nsid, depth, via)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, person_nsid) DO UPDATE SET
			depth = LEAST(crawl_interests.depth, EXCLUDED.depth),
			via = CASE WHEN EXCLUDED.depth < crawl_interests.depth THEN EXCLUDED.via ELSE crawl_interests.via END`,
		interest.UserID, strings.TrimSpace(interest.PersonNSID), interest.Depth, strings.TrimSpace(interest.Via))
	if err != nil {
		return fmt.Errorf("crawl: add interest: %w", err)
	}
	return nil
}

// ForPerson returns every user's depth for a person.
func (repository *Repository) ForPerson(ctx context.Context, person string) ([]crawldomain.InterestDepth, error) {
	if repository == nil || repository.pool == nil {
		return nil, errDatabaseRequired
	}
	if ctx == nil {
		return nil, errContextRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	person = strings.TrimSpace(person)
	if person == "" {
		return nil, crawldomain.ErrInvalidInterest
	}
	rows, err := repository.pool.Query(ctx, `SELECT user_id, depth FROM crawl_interests WHERE person_nsid = $1 ORDER BY user_id`, person)
	if err != nil {
		return nil, fmt.Errorf("crawl: list interests for person: %w", err)
	}
	defer rows.Close()
	interests := make([]crawldomain.InterestDepth, 0)
	for rows.Next() {
		var interest crawldomain.InterestDepth
		if err := rows.Scan(&interest.UserID, &interest.Depth); err != nil {
			return nil, fmt.Errorf("crawl: scan person interest: %w", err)
		}
		interests = append(interests, interest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("crawl: list interests for person: %w", err)
	}
	return interests, nil
}
