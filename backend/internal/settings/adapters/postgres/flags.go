package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/settings/domain"
)

const (
	listFeatureFlagsSQL = `
SELECT name, enabled, audience, user_ids
FROM feature_flags
ORDER BY name`

	userIsAdminSQL = `
SELECT EXISTS (
    SELECT 1
    FROM users
    WHERE id = $1 AND role = 'admin' AND disabled_at IS NULL
)`

	upsertFeatureFlagSQL = `
INSERT INTO feature_flags (name, enabled, audience, user_ids)
VALUES ($1, $2, $3, $4)
ON CONFLICT (name) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    audience = EXCLUDED.audience,
    user_ids = EXCLUDED.user_ids,
    updated_at = now()`
)

var (
	errFlagDatabaseRequired = errors.New("settings: feature flag postgres database is required")
	errFlagContextRequired  = errors.New("settings: feature flag context is required")
)

type flagRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type flagRow interface{ Scan(...any) error }

type flagTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type flagDB interface {
	Query(context.Context, string, ...any) (flagRows, error)
	QueryRow(context.Context, string, ...any) flagRow
	Begin(context.Context) (flagTx, error)
}

type pgxFlagDB struct{ pool *pgxpool.Pool }

func (database pgxFlagDB) Query(ctx context.Context, query string, args ...any) (flagRows, error) {
	return database.pool.Query(ctx, query, args...)
}

func (database pgxFlagDB) QueryRow(ctx context.Context, query string, args ...any) flagRow {
	return database.pool.QueryRow(ctx, query, args...)
}

func (database pgxFlagDB) Begin(ctx context.Context) (flagTx, error) {
	return database.pool.Begin(ctx)
}

// FlagRepository stores the feature flag rows in PostgreSQL.
type FlagRepository struct{ database flagDB }

func NewFlagRepository(pool *pgxpool.Pool) (*FlagRepository, error) {
	if pool == nil {
		return nil, errFlagDatabaseRequired
	}
	return &FlagRepository{database: pgxFlagDB{pool: pool}}, nil
}

func newFlagRepository(database flagDB) *FlagRepository {
	return &FlagRepository{database: database}
}

func (repository *FlagRepository) ListFlags(ctx context.Context) ([]domain.FeatureFlag, error) {
	if err := repository.ready(ctx); err != nil {
		return nil, err
	}
	rows, err := repository.database.Query(ctx, listFeatureFlagsSQL)
	if err != nil {
		return nil, fmt.Errorf("settings: query feature flags: %w", err)
	}
	defer rows.Close()

	flags := make([]domain.FeatureFlag, 0)
	for rows.Next() {
		flag, err := scanFeatureFlag(rows)
		if err != nil {
			return nil, fmt.Errorf("settings: read feature flag: %w", err)
		}
		flags = append(flags, flag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: iterate feature flags: %w", err)
	}
	return flags, nil
}

func (repository *FlagRepository) UserIsAdmin(ctx context.Context, userID int64) (bool, error) {
	if err := repository.ready(ctx); err != nil {
		return false, err
	}
	if userID <= 0 {
		return false, nil
	}
	var isAdmin bool
	if err := repository.database.QueryRow(ctx, userIsAdminSQL, userID).Scan(&isAdmin); err != nil {
		return false, fmt.Errorf("settings: query administrator role: %w", err)
	}
	return isAdmin, nil
}

func (repository *FlagRepository) UpsertFlags(ctx context.Context, flags []domain.FeatureFlag) error {
	if err := repository.ready(ctx); err != nil {
		return err
	}
	if len(flags) == 0 {
		return errors.New("settings: at least one feature flag is required")
	}
	for _, flag := range flags {
		if err := domain.ValidateFeatureFlag(flag); err != nil {
			return err
		}
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("settings: begin feature flag update: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, flag := range flags {
		userIDs := append([]int64{}, flag.UserIDs...)
		if _, err := tx.Exec(ctx, upsertFeatureFlagSQL, flag.Name, flag.Enabled, string(flag.Audience), userIDs); err != nil {
			return fmt.Errorf("settings: save feature flag %q: %w", flag.Name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("settings: commit feature flag update: %w", err)
	}
	return nil
}

func (repository *FlagRepository) ready(ctx context.Context) error {
	if repository == nil || repository.database == nil {
		return errFlagDatabaseRequired
	}
	if ctx == nil {
		return errFlagContextRequired
	}
	return ctx.Err()
}

func scanFeatureFlag(rows interface{ Scan(...any) error }) (domain.FeatureFlag, error) {
	var flag domain.FeatureFlag
	var audience string
	if err := rows.Scan(&flag.Name, &flag.Enabled, &audience, &flag.UserIDs); err != nil {
		return domain.FeatureFlag{}, err
	}
	flag.Audience = domain.FlagAudience(audience)
	if flag.UserIDs == nil {
		flag.UserIDs = []int64{}
	}
	return flag, nil
}
