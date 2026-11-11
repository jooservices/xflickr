// Package postgres implements Home snapshot reads with PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/home/app"
)

var errDatabaseRequired = errors.New("home: database is required")

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Repository reads the contact portion of a Home snapshot.
type Repository struct {
	pool queryer
}

// New creates a Home PostgreSQL repository.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errDatabaseRequired
	}
	return &Repository{pool: pool}, nil
}

// ContactDepths returns direct and discovered contact counts for one user.
func (r *Repository) ContactDepths(ctx context.Context, userID int64) (app.ContactDepthCounts, error) {
	if r == nil || r.pool == nil || ctx == nil || userID <= 0 {
		return app.ContactDepthCounts{}, errors.New("home: repository, context, and user are required")
	}
	const query = `
SELECT
    COALESCE(sum(contact_count) FILTER (WHERE depth = 1), 0)::bigint,
    COALESCE(sum(contact_count) FILTER (WHERE depth > 1), 0)::bigint
FROM (
    SELECT depth, count(*) AS contact_count
    FROM crawl_interests
    WHERE user_id = $1
    GROUP BY depth
) AS contact_depths`
	var counts app.ContactDepthCounts
	if err := r.pool.QueryRow(ctx, query, userID).Scan(&counts.Direct, &counts.Discovered); err != nil {
		return app.ContactDepthCounts{}, fmt.Errorf("home: read contact depths: %w", err)
	}
	return counts, nil
}

var _ app.ContactDepthReader = (*Repository)(nil)
