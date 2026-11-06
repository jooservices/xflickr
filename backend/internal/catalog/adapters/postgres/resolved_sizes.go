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
