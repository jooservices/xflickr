package crypto

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	DefaultRotationBatchSize = 100
	MaxRotationBatchSize     = 10000
)

var sqlIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Target identifies a JSONB column that stores Sealed values. Table may be a
// table name or a schema-qualified table name; IDColumn must identify each
// row uniquely and stably.
type Target struct {
	Table        string
	IDColumn     string
	SealedColumn string
}

// RotationDB provides transactions for rotation batches so a subsequent
// invocation can resume after each committed batch. NewPGXRotationDB adapts a
// pgx pool to this interface.
type RotationDB interface {
	Begin(context.Context) (RotationTx, error)
}

// RotationTx is the small transactional surface used by a rotation batch.
type RotationTx interface {
	Query(context.Context, string, ...any) (RotationRows, error)
	Exec(context.Context, string, ...any) error
	Commit(context.Context) error
	Rollback(context.Context) error
}

// RotationRows is the row-iteration surface used by a rotation batch.
type RotationRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type pgxBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// NewPGXRotationDB adapts a pgx pool to the rotation database interface.
func NewPGXRotationDB(pool interface {
	Begin(context.Context) (pgx.Tx, error)
},
) RotationDB {
	return pgxRotationDB{pool: pool}
}

type pgxRotationDB struct {
	pool pgxBeginner
}

func (database pgxRotationDB) Begin(ctx context.Context) (RotationTx, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return pgxRotationTx{tx: tx}, nil
}

type pgxRotationTx struct {
	tx pgx.Tx
}

func (tx pgxRotationTx) Query(ctx context.Context, query string, args ...any) (RotationRows, error) {
	rows, err := tx.tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (tx pgxRotationTx) Exec(ctx context.Context, query string, args ...any) error {
	_, err := tx.tx.Exec(ctx, query, args...)
	return err
}

func (tx pgxRotationTx) Commit(ctx context.Context) error {
	return tx.tx.Commit(ctx)
}

func (tx pgxRotationTx) Rollback(ctx context.Context) error {
	return tx.tx.Rollback(ctx)
}

// Rotate re-wraps old data keys in the configured targets. Each committed
// batch is durable; if a later batch fails, invoking Rotate again picks up the
// remaining rows because rows already using the active key are excluded.
func Rotate(ctx context.Context, database RotationDB, keyring *Keyring, targets []Target, batchSize int) (int64, error) {
	if database == nil {
		return 0, errors.New("crypto: rotation database is required")
	}
	if keyring == nil || keyring.ActiveKeyID() == "" {
		return 0, errors.New("crypto: rotation keyring is required")
	}
	if len(targets) == 0 {
		return 0, errors.New("crypto: at least one rotation target is required")
	}
	if batchSize < 1 || batchSize > MaxRotationBatchSize {
		return 0, fmt.Errorf("crypto: batch size must be between 1 and %d", MaxRotationBatchSize)
	}
	for _, target := range targets {
		if err := validateTarget(target); err != nil {
			return 0, err
		}
	}

	var total int64
	for _, target := range targets {
		for {
			changed, err := rotateBatch(ctx, database, keyring, target, batchSize)
			if err != nil {
				return total, fmt.Errorf("crypto: rotate %s.%s: %w", target.Table, target.SealedColumn, err)
			}
			total += int64(changed)
			if changed == 0 {
				break
			}
		}
	}
	return total, nil
}

func rotateBatch(ctx context.Context, database RotationDB, keyring *Keyring, target Target, batchSize int) (int, error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	table := quoteTable(target.Table)
	idColumn := quoteIdentifier(target.IDColumn)
	sealedColumn := quoteIdentifier(target.SealedColumn)
	selectSQL := fmt.Sprintf(
		"SELECT %s, %s::text, %s FROM %s WHERE %s IS NOT NULL AND %s->>'key_id' IS DISTINCT FROM $1 ORDER BY 1 LIMIT $2 FOR UPDATE",
		idColumn, idColumn, sealedColumn, table, sealedColumn, sealedColumn,
	)
	rows, err := tx.Query(ctx, selectSQL, keyring.ActiveKeyID(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("select batch: %w", err)
	}
	selected := make([]rotationRow, 0, batchSize)
	for rows.Next() {
		var row rotationRow
		if err := rows.Scan(&row.id, &row.idText, &row.sealed); err != nil {
			rows.Close()
			return 0, fmt.Errorf("read batch: %w", err)
		}
		selected = append(selected, row)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return 0, fmt.Errorf("read batch: %w", rowsErr)
	}
	if len(selected) == 0 {
		return 0, nil
	}

	updateSQL := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2", table, sealedColumn, idColumn)
	for _, row := range selected {
		rotated, err := keyring.Rewrap(row.sealed, RowAAD(target.Table, row.idText))
		if err != nil {
			return 0, fmt.Errorf("rewrap row: %w", err)
		}
		stored, err := rotated.Value()
		if err != nil {
			return 0, fmt.Errorf("encode rotated value: %w", err)
		}
		if err := tx.Exec(ctx, updateSQL, stored, row.id); err != nil {
			return 0, fmt.Errorf("update row: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit batch: %w", err)
	}
	return len(selected), nil
}

type rotationRow struct {
	id     any
	idText string
	sealed Sealed
}

func validateTarget(target Target) error {
	if !validTableName(target.Table) || !sqlIdentifier.MatchString(target.IDColumn) || !sqlIdentifier.MatchString(target.SealedColumn) {
		return errors.New("crypto: invalid rotation target identifier")
	}
	if target.IDColumn == target.SealedColumn {
		return errors.New("crypto: rotation ID and sealed columns must differ")
	}
	return nil
}

func validTableName(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !sqlIdentifier.MatchString(part) {
			return false
		}
	}
	return true
}

func quoteTable(value string) string {
	parts := strings.Split(value, ".")
	for index, part := range parts {
		parts[index] = quoteIdentifier(part)
	}
	return strings.Join(parts, ".")
}

func quoteIdentifier(value string) string {
	return `"` + value + `"`
}
