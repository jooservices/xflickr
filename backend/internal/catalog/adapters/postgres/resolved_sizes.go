package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// ResolvedSizes commits terminal sizes and their continuation event together.
// Repository supplies the read side; Pool supplies independent transactions.
type ResolvedSizes struct {
	*Repository
	Publish func(context.Context, pgx.Tx, app.SizesResolved) error
	Pool    interface {
		Begin(context.Context) (pgx.Tx, error)
	}
}

func (s ResolvedSizes) SetSizes(ctx context.Context, id int64, sizes []byte, status domain.SizesStatus, at time.Time) error {
	if s.Pool == nil || s.Publish == nil || ctx == nil {
		return errors.New("catalog: sizes transaction database and context are required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin resolved sizes: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	repo, err := New(tx)
	if err != nil {
		return err
	}
	if err := repo.SetSizes(ctx, id, sizes, status, at); err != nil {
		return err
	}
	if status == domain.SizesConfirmed || status == domain.SizesUnavailable {
		if err := s.Publish(ctx, tx, app.SizesResolved{PhotoID: id, Status: string(status)}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetAccountSizes stores one account's outcome. Confirmed results publish a
// continuation immediately; unavailable is published only after all eligible
// accounts have been tried.
func (s ResolvedSizes) SetAccountSizes(ctx context.Context, photoID, accountID, userID int64, sizes []byte, status domain.SizesStatus, version time.Time, visibility string, at time.Time) error {
	if s.Pool == nil || s.Publish == nil || ctx == nil || !validAccountSizesUpdate(photoID, accountID, userID, sizes, status, version, visibility, at) {
		return errors.New("catalog: invalid account sizes update")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin account sizes update: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	tag, err := tx.Exec(ctx, `INSERT INTO photo_account_sizes
 (photo_id,account_id,photo_updated_at,photo_visibility,sizes,sizes_status,sizes_checked_at)
SELECT p.id,a.id,$4,$5,$6,$7,$8
FROM photos p JOIN flickr_accounts a ON a.id=$2 AND a.user_id=$3
WHERE p.id=$1 AND p.last_update=$4 AND p.visibility=$5
ON CONFLICT (photo_id,account_id) DO UPDATE SET
 photo_updated_at=EXCLUDED.photo_updated_at,photo_visibility=EXCLUDED.photo_visibility,
 sizes=EXCLUDED.sizes,sizes_status=EXCLUDED.sizes_status,sizes_checked_at=EXCLUDED.sizes_checked_at`,
		photoID, accountID, userID, toPgTime(version), visibility, sizes, string(status), toPgTime(at))
	if err != nil {
		return fmt.Errorf("catalog: store account photo sizes: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("catalog: account sizes photo version or account ownership changed")
	}
	if status == domain.SizesConfirmed {
		if err := s.Publish(ctx, tx, app.SizesResolved{PhotoID: photoID, UserID: userID, Status: string(status)}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func validAccountSizesUpdate(photoID, accountID, userID int64, sizes []byte, status domain.SizesStatus, version time.Time, visibility string, at time.Time) bool {
	return photoID > 0 && accountID > 0 && userID > 0 && jsonDocument(sizes) && !version.IsZero() && visibility != "" && !at.IsZero() && (status == domain.SizesConfirmed || status == domain.SizesUnavailable)
}

// ResolveUserSizes wakes only one user's waiting downloads when no account can
// produce a new result for the current request.
func (s ResolvedSizes) ResolveUserSizes(ctx context.Context, photoID, userID int64, status domain.SizesStatus) error {
	if s.Pool == nil || s.Publish == nil || ctx == nil || photoID <= 0 || userID <= 0 || (status != domain.SizesConfirmed && status != domain.SizesUnavailable) {
		return errors.New("catalog: invalid user sizes continuation")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin user sizes continuation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := s.Publish(ctx, tx, app.SizesResolved{PhotoID: photoID, UserID: userID, Status: string(status)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
