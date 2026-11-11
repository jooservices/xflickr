package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/cluster/app"
	"github.com/jooservices/xflickr/internal/cluster/domain"
)

type pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct{ pool pool }

const findDuplicateEgressSQL = `
SELECT name FROM nodes
WHERE egress_ip = $1::inet AND egress_ip IS NOT NULL AND name <> $2
  AND state = 'online' AND roles @> ARRAY['crawl']::text[]
  AND last_heartbeat_at >= $3::timestamptz - $4::interval
  AND (cooling_until IS NULL OR cooling_until <= $3::timestamptz)
ORDER BY name LIMIT 1`

func New(database *pgxpool.Pool) *Repository { return &Repository{pool: database} }

func (repository *Repository) Create(ctx context.Context, node domain.Node, tokenHash []byte) (domain.Node, error) {
	created, err := scanNode(repository.pool.QueryRow(ctx, `
INSERT INTO nodes (name, roles, state, metrics, token_hash)
VALUES ($1, $2, $3, '{}'::jsonb, $4)
RETURNING name, roles, region, version, COALESCE(host(egress_ip), ''), state,
          cooling_until, last_heartbeat_at, metrics::text`,
		node.Name, node.Roles, string(domain.StateOffline), tokenHash))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.Node{}, app.ErrNodeExists
		}
		return domain.Node{}, fmt.Errorf("cluster: create node: %w", err)
	}
	return created, nil
}

func (repository *Repository) AuthenticateToken(ctx context.Context, tokenHash []byte) (domain.Node, error) {
	node, err := scanNode(repository.pool.QueryRow(ctx, `
SELECT name, roles, region, version, COALESCE(host(egress_ip), ''), state,
       cooling_until, last_heartbeat_at, metrics::text
FROM nodes
WHERE token_hash = $1 AND state <> 'disabled'`, tokenHash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Node{}, app.ErrInvalidToken
		}
		return domain.Node{}, fmt.Errorf("cluster: authenticate node token: %w", err)
	}
	return node, nil
}

// FindDuplicateEgress returns one eligible node sharing node's egress IP.
func (repository *Repository) FindDuplicateEgress(ctx context.Context, node domain.Node, at time.Time) (string, error) {
	if node.EgressIP == "" {
		return "", nil
	}
	warningInterval := pgtype.Interval{Microseconds: domain.WarningAfter.Microseconds()}
	var duplicate string
	err := repository.pool.QueryRow(ctx, findDuplicateEgressSQL, node.EgressIP, node.Name, at.UTC(), warningInterval).Scan(&duplicate)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cluster: find duplicate egress IP: %w", err)
	}
	return duplicate, nil
}

func (repository *Repository) Heartbeat(ctx context.Context, name string, input app.HeartbeatInput, at time.Time) (domain.Node, error) {
	node, err := scanNode(repository.pool.QueryRow(ctx, `
UPDATE nodes
SET version = $2,
    egress_ip = NULLIF($3, '')::inet,
    metrics = $4::jsonb,
    last_heartbeat_at = $5,
    state = CASE WHEN cooling_until > $5 THEN 'cooling' ELSE 'online' END
WHERE name = $1 AND state <> 'disabled'
RETURNING name, roles, region, version, COALESCE(host(egress_ip), ''), state,
          cooling_until, last_heartbeat_at, metrics::text`,
		name, input.Version, input.EgressIP, string(input.Metrics), at))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Node{}, app.ErrInvalidToken
		}
		return domain.Node{}, fmt.Errorf("cluster: update node heartbeat: %w", err)
	}
	return node, nil
}

func scanNode(row pgx.Row) (domain.Node, error) {
	var node domain.Node
	var coolingUntil, lastHeartbeatAt pgtype.Timestamptz
	var metrics string
	if err := row.Scan(&node.Name, &node.Roles, &node.Region, &node.Version, &node.EgressIP, &node.State, &coolingUntil, &lastHeartbeatAt, &metrics); err != nil {
		return domain.Node{}, err
	}
	if coolingUntil.Valid {
		value := coolingUntil.Time
		node.CoolingUntil = &value
	}
	if lastHeartbeatAt.Valid {
		value := lastHeartbeatAt.Time
		node.LastHeartbeatAt = &value
	}
	node.Metrics = []byte(metrics)
	return node, nil
}
