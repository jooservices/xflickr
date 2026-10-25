package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

type dbPool interface {
	Begin(context.Context) (transaction, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (rows, error)
	QueryRow(context.Context, string, ...any) row
}

type pgxPool interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type transaction interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type row interface{ Scan(...any) error }

type Store struct{ pool dbPool }

type (
	poolAdapter struct{ pool pgxPool }
	txAdapter   struct{ tx pgx.Tx }
)

func (p poolAdapter) Begin(ctx context.Context) (transaction, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return txAdapter{tx: tx}, nil
}

func (p poolAdapter) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, query, args...)
}

func (p poolAdapter) Query(ctx context.Context, query string, args ...any) (rows, error) {
	return p.pool.Query(ctx, query, args...)
}

func (p poolAdapter) QueryRow(ctx context.Context, query string, args ...any) row {
	return p.pool.QueryRow(ctx, query, args...)
}

func (t txAdapter) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, query, args...)
}
func (t txAdapter) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t txAdapter) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: poolAdapter{pool: pool}} }

const planColumns = `id,name,weight,max_depth_cap,max_contacts,ai_quota_per_day,zip_ttl_hours,resource_share::float8`

func (s *Store) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+planColumns+` FROM plans ORDER BY weight,name`)
	if err != nil {
		return nil, fmt.Errorf("schedule: list plans: %w", err)
	}
	defer rows.Close()
	plans := []domain.Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("schedule: scan plan: %w", err)
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

func (s *Store) SavePlans(ctx context.Context, plans []domain.Plan) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, p := range plans {
		_, err = tx.Exec(ctx, `INSERT INTO plans(name,weight,max_depth_cap,max_contacts,ai_quota_per_day,zip_ttl_hours,resource_share) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(name) DO UPDATE SET weight=EXCLUDED.weight,max_depth_cap=EXCLUDED.max_depth_cap,max_contacts=EXCLUDED.max_contacts,ai_quota_per_day=EXCLUDED.ai_quota_per_day,zip_ttl_hours=EXCLUDED.zip_ttl_hours,resource_share=EXCLUDED.resource_share`, p.Name, p.Weight, p.MaxDepthCap, p.MaxContacts, p.AIQuotaPerDay, p.ZipTTLHours, p.ResourceShare)
		if err != nil {
			return fmt.Errorf("schedule: save plan %q: %w", p.Name, err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) PlanForUser(ctx context.Context, userID int64) (domain.Plan, error) {
	p, err := scanPlan(s.pool.QueryRow(ctx, `SELECT p.`+`id,p.name,p.weight,p.max_depth_cap,p.max_contacts,p.ai_quota_per_day,p.zip_ttl_hours,p.resource_share::float8 FROM users u JOIN plans p ON p.id=u.plan_id WHERE u.id=$1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Plan{}, app.ErrUserNotFound
	}
	if err != nil {
		return domain.Plan{}, fmt.Errorf("schedule: get user plan: %w", err)
	}
	return p, nil
}

func (s *Store) AssignUserPlan(ctx context.Context, userID, planID int64) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plans WHERE id=$1)`, planID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return app.ErrPlanNotFound
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET plan_id=$2 WHERE id=$1`, userID, planID)
	if err != nil {
		return fmt.Errorf("schedule: assign user plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return app.ErrUserNotFound
	}
	return nil
}

func scanPlan(r row) (domain.Plan, error) {
	var p domain.Plan
	err := r.Scan(&p.ID, &p.Name, &p.Weight, &p.MaxDepthCap, &p.MaxContacts, &p.AIQuotaPerDay, &p.ZipTTLHours, &p.ResourceShare)
	return p, err
}
