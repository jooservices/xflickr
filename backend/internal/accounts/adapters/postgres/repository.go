// Package postgres stores API keys and publishes key.deleted with the delete.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/outbox"
)

const (
	nextIDSQL = `SELECT nextval(pg_get_serial_sequence('api_keys', 'id'))`

	insertSQL = `
INSERT INTO api_keys (
    id, user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING created_at`

	listSQL = `
SELECT id, user_id, label, key_public, per_hour_limit, per_second_limit, burst, status, created_at
FROM api_keys
WHERE user_id = $1
ORDER BY id`

	getSQL = `
SELECT id, user_id, label, key_public, per_hour_limit, per_second_limit, burst, status, created_at
FROM api_keys
WHERE id = $1 AND user_id = $2`

	lockSQL = getSQL + ` FOR UPDATE`

	updateSQL = `
UPDATE api_keys
SET label = $1, per_hour_limit = $2, per_second_limit = $3, burst = $4, secret_sealed = COALESCE($5, secret_sealed)
WHERE id = $6 AND user_id = $7`

	deleteSQL = `
DELETE FROM api_keys
WHERE id = $1 AND user_id = $2`
)

// markSQL uses the domain status constants so the stored values cannot drift.
// A revoked account stays revoked: only an ok account needs to reconnect.
var markSQL = `
UPDATE flickr_accounts
SET status = '` + string(domain.AccountReconnectRequired) + `'
WHERE key_id = $1 AND status = '` + string(domain.AccountOK) + `'`

// Repository persists API keys. Read queries do not select secret_sealed.
type Repository struct {
	unit *db.UnitOfWork
}

// New creates a repository backed by database. database is typically a pgx pool.
func New(database db.Beginner) (*Repository, error) {
	if database == nil {
		return nil, errors.New("accounts: postgres database is required")
	}
	return &Repository{unit: db.NewUnitOfWork(database)}, nil
}

// Create allocates an id, seals the secret with api_keys:<id>, and inserts one row.
func (r *Repository) Create(ctx context.Context, key domain.APIKey, seal func(int64) (crypto.Sealed, error)) (domain.APIKey, error) {
	if err := r.ready(ctx); err != nil {
		return domain.APIKey{}, err
	}
	if seal == nil {
		return domain.APIKey{}, errors.New("accounts: sealer is required")
	}
	var created domain.APIKey
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, nextIDSQL).Scan(&id); err != nil {
			return fmt.Errorf("accounts: allocate api key id: %w", err)
		}
		sealed, err := seal(id)
		if err != nil {
			return err
		}
		key.ID = id
		if key.Status == "" {
			key.Status = domain.StatusValid
		}
		if err := tx.QueryRow(ctx, insertSQL,
			key.ID, key.UserID, key.Label, key.PublicKey, sealed,
			key.PerHourLimit, key.PerSecondLimit, key.Burst, string(key.Status),
		).Scan(&key.CreatedAt); err != nil {
			if isUniqueViolation(err) {
				return app.ErrKeyExists
			}
			return fmt.Errorf("accounts: insert api key: %w", err)
		}
		created = key
		return nil
	})
	if err != nil {
		return domain.APIKey{}, err
	}
	return created, nil
}

// Update locks the caller's key, applies the change, and marks connected
// accounts when a new secret is stored.
func (r *Repository) Update(ctx context.Context, userID, id int64, apply func(domain.APIKey) (domain.APIKey, *crypto.Sealed, error)) (domain.APIKey, error) {
	if err := r.ready(ctx); err != nil {
		return domain.APIKey{}, err
	}
	if apply == nil {
		return domain.APIKey{}, errors.New("accounts: update is required")
	}
	var updated domain.APIKey
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		current, err := scanKey(tx.QueryRow(ctx, lockSQL, id, userID))
		if err != nil {
			return err
		}
		next, sealed, err := apply(current)
		if err != nil {
			return err
		}
		next.ID = current.ID
		next.UserID = current.UserID
		next.PublicKey = current.PublicKey
		next.Status = current.Status
		next.CreatedAt = current.CreatedAt
		var secret any
		if sealed != nil {
			secret = *sealed
		}
		tag, err := tx.Exec(ctx, updateSQL, next.Label, next.PerHourLimit, next.PerSecondLimit, next.Burst, secret, id, userID)
		if err != nil {
			return fmt.Errorf("accounts: update api key: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return app.ErrNotFound
		}
		if sealed != nil {
			if _, err := tx.Exec(ctx, markSQL, id); err != nil {
				return fmt.Errorf("accounts: mark connected accounts: %w", err)
			}
		}
		updated = next
		return nil
	})
	if err != nil {
		return domain.APIKey{}, err
	}
	return updated, nil
}

// Delete removes the key and publishes key.deleted in the same transaction.
func (r *Repository) Delete(ctx context.Context, userID, id int64) error {
	if err := r.ready(ctx); err != nil {
		return err
	}
	return r.unit.Do(ctx, func(tx db.Tx) error {
		tag, err := tx.Exec(ctx, deleteSQL, id, userID)
		if err != nil {
			return fmt.Errorf("accounts: delete api key: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return app.ErrNotFound
		}
		return outbox.PublishContext(ctx, tx, domain.DeletedTopic, strconv.FormatInt(id, 10), domain.Deleted{
			ID:     id,
			UserID: userID,
		})
	})
}

// List returns the user's keys without their sealed secrets.
func (r *Repository) List(ctx context.Context, userID int64) ([]domain.APIKey, error) {
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	var keys []domain.APIKey
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		rows, err := tx.Query(ctx, listSQL, userID)
		if err != nil {
			return fmt.Errorf("accounts: list api keys: %w", err)
		}
		defer rows.Close()
		loaded := make([]domain.APIKey, 0)
		for rows.Next() {
			key, err := scanKey(rows)
			if err != nil {
				return err
			}
			loaded = append(loaded, key)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("accounts: list api keys: %w", err)
		}
		keys = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	if keys == nil {
		keys = []domain.APIKey{}
	}
	return keys, nil
}

// Get returns one key owned by the user. The sealed secret is not loaded.
func (r *Repository) Get(ctx context.Context, userID, id int64) (domain.APIKey, error) {
	if err := r.ready(ctx); err != nil {
		return domain.APIKey{}, err
	}
	var key domain.APIKey
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		loaded, err := scanKey(tx.QueryRow(ctx, getSQL, id, userID))
		if err != nil {
			return err
		}
		key = loaded
		return nil
	})
	if err != nil {
		return domain.APIKey{}, err
	}
	return key, nil
}

func (r *Repository) ready(ctx context.Context) error {
	if r == nil || r.unit == nil {
		return errors.New("accounts: postgres database is required")
	}
	if ctx == nil {
		return errors.New("accounts: context is required")
	}
	return ctx.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanKey(row scanner) (domain.APIKey, error) {
	var key domain.APIKey
	var status string
	err := row.Scan(
		&key.ID, &key.UserID, &key.Label, &key.PublicKey,
		&key.PerHourLimit, &key.PerSecondLimit, &key.Burst, &status, &key.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.APIKey{}, app.ErrNotFound
	}
	if err != nil {
		return domain.APIKey{}, err
	}
	key.Status = domain.Status(status)
	if !key.Status.Valid() {
		return domain.APIKey{}, fmt.Errorf("accounts: stored api key %d has status %q", key.ID, status)
	}
	return key, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
