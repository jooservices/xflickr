package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/cluster/app"
	"github.com/jooservices/xflickr/internal/cluster/domain"
)

type assignmentTransactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

var _ app.AssignmentRepository = (*Repository)(nil)

// AssignInitial inserts an eligible key assignment if one does not already
// exist, then reads the stored assignment on the same READ COMMITTED transaction.
func (repository *Repository) AssignInitial(ctx context.Context, keyID int64, node string, now time.Time) (app.Assignment, error) {
	beginner, ok := repository.pool.(assignmentTransactionBeginner)
	if !ok {
		return app.Assignment{}, errors.New("cluster: assignment transaction is unavailable")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return app.Assignment{}, fmt.Errorf("cluster: begin key assignment: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO key_assignments (key_id, node, since, reason)
SELECT k.id, n.name, $3, 'initial'
FROM api_keys AS k CROSS JOIN nodes AS n
WHERE k.id = $1 AND k.status = 'valid' AND n.name = $2
  AND n.state = 'online' AND 'crawl' = ANY(n.roles)
  AND n.last_heartbeat_at >= $4
  AND (n.cooling_until IS NULL OR n.cooling_until <= $3)
ON CONFLICT (key_id) DO NOTHING`, keyID, node, now, now.Add(-domain.WarningAfter)); err != nil {
		return app.Assignment{}, rollbackAssignment(tx, fmt.Errorf("cluster: insert initial key assignment: %w", err))
	}
	assignment, err := scanAssignment(tx.QueryRow(ctx, `
SELECT key_id, node, since, reason
FROM key_assignments WHERE key_id = $1`, keyID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Assignment{}, rollbackAssignment(tx, app.ErrAssignmentUnavailable)
		}
		return app.Assignment{}, rollbackAssignment(tx, fmt.Errorf("cluster: read key assignment: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Assignment{}, fmt.Errorf("cluster: commit key assignment: %w", err)
	}
	return assignment, nil
}

// GetAssignment returns a key's stored initial assignment.
func (repository *Repository) GetAssignment(ctx context.Context, keyID int64) (app.Assignment, error) {
	assignment, err := scanAssignment(repository.pool.QueryRow(ctx, `
SELECT key_id, node, since, reason
FROM key_assignments WHERE key_id = $1`, keyID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Assignment{}, app.ErrAssignmentUnavailable
		}
		return app.Assignment{}, fmt.Errorf("cluster: read key assignment: %w", err)
	}
	return assignment, nil
}

func scanAssignment(row pgx.Row) (app.Assignment, error) {
	var assignment app.Assignment
	err := row.Scan(&assignment.KeyID, &assignment.Node, &assignment.Since, &assignment.Reason)
	if err != nil {
		return app.Assignment{}, err
	}
	return assignment, nil
}

func rollbackAssignment(tx pgx.Tx, cause error) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tx.Rollback(rollbackCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return errors.Join(cause, fmt.Errorf("cluster: rollback key assignment: %w", err))
	}
	return cause
}
