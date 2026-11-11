// Package postgres stores user-owned cloud sync targets.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/sync/app"
)

const (
	nextTargetIDSQL = `SELECT nextval(pg_get_serial_sequence('sync_targets', 'id'))`

	insertTargetSQL = `
INSERT INTO sync_targets (id,user_id,provider,label,token_sealed,config,status)
VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)`

	getSQL = `
SELECT id,user_id,provider,label,config,status
FROM sync_targets WHERE id=$1 AND user_id=$2`

	listSQL = `
SELECT id,user_id,provider,label,config,status
FROM sync_targets WHERE user_id=$1 AND id>$2 ORDER BY id LIMIT $3`
)

// Repository stores targets in PostgreSQL and omits sealed tokens from reads.
type Repository struct {
	unit *db.UnitOfWork
}

// New creates a target repository backed by database.
func New(database db.Beginner) (*Repository, error) {
	if database == nil {
		return nil, errors.New("sync: postgres database is required")
	}
	return &Repository{unit: db.NewUnitOfWork(database)}, nil
}

// Create allocates an ID, seals the token for that ID, and inserts one target.
func (r *Repository) Create(ctx context.Context, target app.Target, seal func(int64) (crypto.Sealed, error)) (app.Target, error) {
	if err := r.ready(ctx); err != nil {
		return app.Target{}, err
	}
	if seal == nil {
		return app.Target{}, errors.New("sync: target sealer is required")
	}
	var created app.Target
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, nextTargetIDSQL).Scan(&id); err != nil {
			return fmt.Errorf("sync: allocate target id: %w", err)
		}
		sealed, err := seal(id)
		if err != nil {
			return err
		}
		encoded, err := sealed.Value()
		if err != nil {
			return fmt.Errorf("sync: encode sealed target token: %w", err)
		}
		sealedBytes := encoded.([]byte)
		if _, err := tx.Exec(ctx, insertTargetSQL,
			id, target.UserID, target.Provider, target.Label, sealedBytes, string(target.Config), target.Status,
		); err != nil {
			return fmt.Errorf("sync: insert target: %w", err)
		}
		target.ID = id
		created = target
		return nil
	})
	if err != nil {
		return app.Target{}, err
	}
	return created, nil
}

// Get returns metadata for a target owned by userID without selecting its token.
func (r *Repository) Get(ctx context.Context, userID, id int64) (app.Target, error) {
	if err := r.ready(ctx); err != nil {
		return app.Target{}, err
	}
	var target app.Target
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		loaded, err := scanTarget(tx.QueryRow(ctx, getSQL, id, userID))
		if err != nil {
			return err
		}
		target = loaded
		return nil
	})
	if err != nil {
		return app.Target{}, fmt.Errorf("sync: get target: %w", err)
	}
	return target, nil
}

// List returns one bounded page of targets owned by userID after afterID.
func (r *Repository) List(ctx context.Context, userID, afterID int64, limit int) ([]app.Target, error) {
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	targets := make([]app.Target, 0)
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		rows, err := tx.Query(ctx, listSQL, userID, afterID, limit)
		if err != nil {
			return fmt.Errorf("sync: query targets: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			target, err := scanTarget(rows)
			if err != nil {
				return err
			}
			targets = append(targets, target)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("sync: iterate targets: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sync: list targets: %w", err)
	}
	return targets, nil
}

func (r *Repository) ready(ctx context.Context) error {
	if r == nil || r.unit == nil {
		return errors.New("sync: postgres database is required")
	}
	if ctx == nil {
		return errors.New("sync: context is required")
	}
	return ctx.Err()
}

type targetScanner interface {
	Scan(...any) error
}

func scanTarget(row targetScanner) (app.Target, error) {
	var target app.Target
	var config []byte
	err := row.Scan(&target.ID, &target.UserID, &target.Provider, &target.Label, &config, &target.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Target{}, app.ErrTargetNotFound
	}
	if err != nil {
		return app.Target{}, fmt.Errorf("sync: scan target: %w", err)
	}
	target.Config = json.RawMessage(config)
	return target, nil
}

var _ app.TargetRepository = (*Repository)(nil)
