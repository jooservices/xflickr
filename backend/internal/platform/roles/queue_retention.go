package roles

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/queue"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

type effectiveSettingsReader interface {
	Effective(context.Context, int64) (settingsdomain.Effective, error)
}

func riverRetention(ctx context.Context, settings effectiveSettingsReader) (queue.Retention, error) {
	if ctx == nil || settings == nil {
		return queue.Retention{}, errors.New("roles: queue retention settings are required")
	}
	effective, err := settings.Effective(ctx, 0)
	if err != nil {
		return queue.Retention{}, fmt.Errorf("roles: load queue retention settings: %w", err)
	}
	return queue.Retention{
		Completed: effective.QueueCompletedRetention,
		Discarded: effective.QueueDiscardedRetention,
	}, nil
}

var _ effectiveSettingsReader = (*settingsapp.Service)(nil)

// newRiverWithSettings builds a River adapter whose cleaner windows come from
// the live settings, so every role applies the same retention.
func newRiverWithSettings(ctx context.Context, pool *pgxpool.Pool, queues map[string]int) (*queue.River, error) {
	if pool == nil {
		return nil, errors.New("roles: PostgreSQL pool is required")
	}
	repository, err := settingspostgres.New(pool)
	if err != nil {
		return nil, err
	}
	retention, err := riverRetention(ctx, settingsapp.New(repository))
	if err != nil {
		// Cleaner windows are tuning, not correctness: start with the River
		// defaults instead of keeping the role down while settings recover.
		slog.WarnContext(ctx, "roles: queue retention settings unavailable; using defaults", "error", err)
		retention = queue.Retention{}
	}
	return queue.NewRiverWithRetention(pool, queues, retention)
}
