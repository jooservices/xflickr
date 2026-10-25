package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/sharing/app"
	"github.com/jooservices/xflickr/internal/sharing/domain"
)

type settingTx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type Repository struct {
	run func(context.Context, func(settingTx) error) error
}

var _ app.Repository = (*Repository)(nil)

func New(pool db.Beginner) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("sharing: database is required")
	}
	run := func(ctx context.Context, fn func(settingTx) error) error {
		return db.NewUnitOfWork(pool).Do(ctx, func(tx db.Tx) error { return fn(tx) })
	}
	return &Repository{run: run}, nil
}

func (r *Repository) Set(ctx context.Context, setting domain.Setting) (domain.Setting, error) {
	if r == nil || r.run == nil || ctx == nil || setting.UserID <= 0 {
		return domain.Setting{}, errors.New("sharing: database, context and user are required")
	}
	var result domain.Setting
	err := r.run(ctx, func(tx settingTx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO sharing_settings (user_id, enabled, changed_at) VALUES ($1, $2, now())
			ON CONFLICT (user_id) DO UPDATE SET enabled=EXCLUDED.enabled, changed_at=now()
			WHERE sharing_settings.enabled IS DISTINCT FROM EXCLUDED.enabled
			RETURNING user_id, enabled, changed_at`, setting.UserID, setting.Enabled).Scan(&result.UserID, &result.Enabled, &result.ChangedAt); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("sharing: save setting: %w", err)
			}
			// A no-op update returns no row; read the existing setting while retaining atomicity.
			if err := tx.QueryRow(ctx, `SELECT user_id, enabled, changed_at FROM sharing_settings WHERE user_id=$1`, setting.UserID).Scan(&result.UserID, &result.Enabled, &result.ChangedAt); err != nil {
				return fmt.Errorf("sharing: save setting: %w", err)
			}
			return nil
		}
		payload, err := json.Marshal(domain.Changed(result))
		if err != nil {
			return fmt.Errorf("sharing: encode changed event: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_events (topic, key, payload) VALUES ($1, $2, $3)`, domain.ChangedTopic, fmt.Sprint(setting.UserID), payload); err != nil {
			return fmt.Errorf("sharing: publish changed event: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Setting{}, err
	}
	return result, nil
}
