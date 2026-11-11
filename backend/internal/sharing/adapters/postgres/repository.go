package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	homeapp "github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/sharing/app"
	"github.com/jooservices/xflickr/internal/sharing/domain"
)

type settingTx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type Repository struct {
	run   func(context.Context, func(settingTx) error) error
	queue queue.JobQueue
}

var (
	_ app.Repository = (*Repository)(nil)
	_ app.Reader     = (*Repository)(nil)
)

func New(pool db.Beginner, refreshQueues ...queue.JobQueue) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("sharing: database is required")
	}
	if len(refreshQueues) > 1 || len(refreshQueues) == 1 && refreshQueues[0] == nil {
		return nil, errors.New("sharing: at most one non-nil refresh queue is allowed")
	}
	run := func(ctx context.Context, fn func(settingTx) error) error {
		return db.NewUnitOfWork(pool).Do(ctx, func(tx db.Tx) error { return fn(tx) })
	}
	repository := &Repository{run: run}
	if len(refreshQueues) == 1 {
		repository.queue = refreshQueues[0]
	}
	return repository, nil
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
		if _, err := tx.Exec(ctx, `UPDATE user_home_stats SET invalidated_at = statement_timestamp()`); err != nil {
			return fmt.Errorf("sharing: invalidate Home counts: %w", err)
		}
		payload, err := json.Marshal(domain.Changed(result))
		if err != nil {
			return fmt.Errorf("sharing: encode changed event: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_events (topic, key, payload) VALUES ($1, $2, $3)`, domain.ChangedTopic, fmt.Sprint(setting.UserID), payload); err != nil {
			return fmt.Errorf("sharing: publish changed event: %w", err)
		}
		if r.queue != nil {
			transaction, ok := tx.(pgx.Tx)
			if !ok {
				return errors.New("sharing: refresh queue requires a PostgreSQL transaction")
			}
			jobArgs, err := json.Marshal(homeapp.RefreshJob{UserID: setting.UserID})
			if err != nil {
				return fmt.Errorf("sharing: encode Home refresh job: %w", err)
			}
			// Each setting change needs its own wake-up; a unique running job
			// could finish just before this transaction commits.
			if err := r.queue.Enqueue(ctx, transaction, queue.Job{Kind: homeapp.RefreshJobKind, Args: jobArgs}, queue.Options{
				Queue: "home", MaxAttempts: 5, LeaseTimeout: time.Minute, UniqueByArgs: true, UniqueByQueue: true,
			}); err != nil {
				return fmt.Errorf("sharing: enqueue Home refresh: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.Setting{}, err
	}
	return result, nil
}

// Get loads a user's sharing setting, defaulting to disabled when it has never
// been changed.
func (r *Repository) Get(ctx context.Context, userID int64) (domain.Setting, error) {
	if r == nil || r.run == nil || ctx == nil || userID <= 0 {
		return domain.Setting{}, errors.New("sharing: database, context and user are required")
	}
	result := domain.Setting{UserID: userID}
	err := r.run(ctx, func(tx settingTx) error {
		err := tx.QueryRow(ctx, `SELECT user_id, enabled, changed_at FROM sharing_settings WHERE user_id=$1`, userID).Scan(&result.UserID, &result.Enabled, &result.ChangedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("sharing: load setting: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Setting{}, fmt.Errorf("sharing: read setting: %w", err)
	}
	return result, nil
}
