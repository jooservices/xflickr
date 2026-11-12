package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/settings/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/settings/app"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

var (
	errFlagDatabaseRequired = errors.New("settings: feature flag postgres database is required")
	errFlagContextRequired  = errors.New("settings: feature flag context is required")
)

// flagQueries is the set of generated queries that FlagRepository calls.
type flagQueries interface {
	ListFeatureFlags(ctx context.Context) ([]gen.ListFeatureFlagsRow, error)
	GetFeatureFlag(ctx context.Context, name string) (gen.GetFeatureFlagRow, error)
	UserIsAdmin(ctx context.Context, userID int64) (bool, error)
	UpsertFeatureFlag(ctx context.Context, arg gen.UpsertFeatureFlagParams) error
}

// flagTransactions starts the transaction that groups the upserts of one update.
type flagTransactions interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

var (
	_ flagQueries               = (*gen.Queries)(nil)
	_ flagTransactions          = (*pgxpool.Pool)(nil)
	_ app.FeatureFlagRepository = (*FlagRepository)(nil)
)

// FlagRepository stores the feature flag rows in PostgreSQL.
type FlagRepository struct {
	queries flagQueries
	txs     flagTransactions
	inTx    func(pgx.Tx) flagQueries
}

// NewFlagRepository wires the generated queries to the pool.
func NewFlagRepository(pool *pgxpool.Pool) (*FlagRepository, error) {
	if pool == nil {
		return nil, errFlagDatabaseRequired
	}
	queries := gen.New(pool)
	return newFlagRepository(queries, pool, func(tx pgx.Tx) flagQueries { return queries.WithTx(tx) }), nil
}

func newFlagRepository(queries flagQueries, txs flagTransactions, inTx func(pgx.Tx) flagQueries) *FlagRepository {
	return &FlagRepository{queries: queries, txs: txs, inTx: inTx}
}

func (repository *FlagRepository) ListFlags(ctx context.Context) ([]domain.FeatureFlag, error) {
	if err := repository.ready(ctx); err != nil {
		return nil, err
	}
	rows, err := repository.queries.ListFeatureFlags(ctx)
	if err != nil {
		return nil, fmt.Errorf("settings: query feature flags: %w", err)
	}
	flags := make([]domain.FeatureFlag, 0, len(rows))
	for _, row := range rows {
		flags = append(flags, toFeatureFlag(row.Name, row.Enabled, row.Audience, row.UserIds))
	}
	return flags, nil
}

// GetFlag reads one feature flag by its primary key.
func (repository *FlagRepository) GetFlag(ctx context.Context, name string) (domain.FeatureFlag, error) {
	if err := repository.ready(ctx); err != nil {
		return domain.FeatureFlag{}, err
	}
	row, err := repository.queries.GetFeatureFlag(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeatureFlag{}, app.ErrFeatureFlagNotFound
	}
	if err != nil {
		return domain.FeatureFlag{}, fmt.Errorf("settings: query feature flag %q: %w", name, err)
	}
	return toFeatureFlag(row.Name, row.Enabled, row.Audience, row.UserIds), nil
}

func (repository *FlagRepository) UserIsAdmin(ctx context.Context, userID int64) (bool, error) {
	if err := repository.ready(ctx); err != nil {
		return false, err
	}
	if userID <= 0 {
		return false, nil
	}
	isAdmin, err := repository.queries.UserIsAdmin(ctx, userID)
	if err != nil {
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
	tx, err := repository.txs.Begin(ctx)
	if err != nil {
		return fmt.Errorf("settings: begin feature flag update: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := repository.inTx(tx)
	for _, flag := range flags {
		arg := gen.UpsertFeatureFlagParams{
			Name:     flag.Name,
			Enabled:  flag.Enabled,
			Audience: string(flag.Audience),
			UserIds:  append([]int64{}, flag.UserIDs...),
		}
		if err := queries.UpsertFeatureFlag(ctx, arg); err != nil {
			return fmt.Errorf("settings: save feature flag %q: %w", flag.Name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("settings: commit feature flag update: %w", err)
	}
	return nil
}

func (repository *FlagRepository) ready(ctx context.Context) error {
	if repository == nil || repository.queries == nil {
		return errFlagDatabaseRequired
	}
	if ctx == nil {
		return errFlagContextRequired
	}
	return ctx.Err()
}

// toFeatureFlag maps a generated row to the domain flag; a NULL array reads as no user ids.
func toFeatureFlag(name string, enabled bool, audience string, userIDs []int64) domain.FeatureFlag {
	if userIDs == nil {
		userIDs = []int64{}
	}
	return domain.FeatureFlag{
		Name:     name,
		Enabled:  enabled,
		Audience: domain.FlagAudience(audience),
		UserIDs:  userIDs,
	}
}
