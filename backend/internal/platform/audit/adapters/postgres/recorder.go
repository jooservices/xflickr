package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Recorder persists administrator actions in PostgreSQL.
type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type Recorder struct{ pool executor }

func New(pool *pgxpool.Pool) *Recorder { return &Recorder{pool: pool} }

func (r *Recorder) Record(ctx context.Context, actor, action, target string, detail map[string]any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("audit: encode detail: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO audit_events(actor,action,target,detail) VALUES($1,$2,$3,$4::jsonb)`, actor, action, target, string(encoded)); err != nil {
		return fmt.Errorf("audit: record event: %w", err)
	}
	return nil
}
