// Package postgres implements the catalog repository with PostgreSQL.
package postgres

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/catalog/app"
)

// Repository stores catalog records.
type Repository struct {
	queries *gen.Queries
	db      gen.DBTX
}

var _ app.Repository = (*Repository)(nil)

// New creates a catalog repository backed by a pgx connection or pool.
func New(database gen.DBTX) (*Repository, error) {
	if database == nil {
		return nil, errors.New("catalog: postgres database is required")
	}
	return &Repository{queries: gen.New(database), db: database}, nil
}

func validateBatchSize(size int) error {
	if size > UpsertPeopleBatchMax {
		return errors.New("catalog: batch exceeds 500 rows")
	}
	return nil
}

func validSizesStatus(status string) bool {
	return status == "none" || status == "provisional" || status == "confirmed" || status == "unavailable"
}

func jsonDocument(value []byte) bool { return json.Valid(value) }

func jsonOrObject(value []byte) []byte {
	if len(value) == 0 {
		return []byte(`{}`)
	}
	return value
}

func jsonOrArray(value []byte) []byte {
	if len(value) == 0 {
		return []byte(`[]`)
	}
	return value
}

func pgTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return toPgTime(*value)
}

func toPgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
