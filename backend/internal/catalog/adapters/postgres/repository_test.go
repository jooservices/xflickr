package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

func TestRepositoryRejectsInvalidCallsBeforeDatabaseAccess(t *testing.T) {
	if _, err := postgres.New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
	repository, err := postgres.New(disconnectedDB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := repository.UpsertPeople(ctx, nil); err != nil {
		t.Fatalf("empty people batch: %v", err)
	}
	if err := repository.UpsertPhotos(ctx, nil); err != nil {
		t.Fatalf("empty photo batch: %v", err)
	}
	tooManyPeople := make([]domain.Person, postgres.UpsertPeopleBatchMax+1)
	tooManyPhotos := make([]domain.Photo, postgres.UpsertPeopleBatchMax+1)
	if err := repository.UpsertPeople(ctx, tooManyPeople); err == nil {
		t.Fatal("oversized people batch succeeded")
	}
	if err := repository.UpsertPhotos(ctx, tooManyPhotos); err == nil {
		t.Fatal("oversized photo batch succeeded")
	}
	if err := repository.UpsertPeople(ctx, []domain.Person{{NSID: "id"}}); err == nil {
		t.Fatal("person with empty username succeeded")
	}
	invalidPhoto := domain.Photo{ID: 1, OwnerNSID: "owner", LastUpdate: time.Now(), Visibility: "invalid", Raw: []byte(`{}`)}
	if err := repository.UpsertPhotos(ctx, []domain.Photo{invalidPhoto}); err == nil {
		t.Fatal("invalid photo succeeded")
	}
	if err := repository.SetSizes(ctx, 0, []byte(`[]`), domain.SizesConfirmed, time.Now()); err == nil {
		t.Fatal("invalid sizes update succeeded")
	}
	if err := repository.SetSizes(ctx, 1, []byte(`bad`), domain.SizesConfirmed, time.Now()); err == nil {
		t.Fatal("malformed sizes update succeeded")
	}
	if err := repository.MarkTombstone(ctx, 0, time.Now()); err == nil {
		t.Fatal("invalid tombstone succeeded")
	}
}

func TestLoadSizesState(t *testing.T) {
	updated := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	checked := updated.Add(time.Minute)
	db := &photoSizesDB{row: photoSizesRow{values: []any{int64(8), updated, "confirmed", pgtype.Timestamptz{Time: checked, Valid: true}}}}
	repository, err := postgres.New(db)
	if err != nil {
		t.Fatal(err)
	}
	photo, exists, err := repository.LoadSizes(context.Background(), 8)
	if err != nil || !exists || photo.ID != 8 || photo.SizesStatus != domain.SizesConfirmed || photo.SizesCheckedAt == nil || !photo.SizesCheckedAt.Equal(checked) {
		t.Fatalf("LoadSizes() = %+v, %v, %v", photo, exists, err)
	}
	if _, exists, err := repository.LoadSizes(context.Background(), 0); err == nil || exists {
		t.Fatal("LoadSizes accepted invalid id")
	}
	if _, exists, err := repository.LoadSizes(nil, 8); err == nil || exists {
		t.Fatal("LoadSizes accepted nil context")
	}
	if _, exists, err := (*postgres.Repository)(nil).LoadSizes(context.Background(), 8); err == nil || exists {
		t.Fatal("LoadSizes accepted nil repository")
	}
	db.row = photoSizesRow{err: pgx.ErrNoRows}
	if _, exists, err := repository.LoadSizes(context.Background(), 8); err != nil || exists {
		t.Fatalf("missing photo = exists:%v err:%v", exists, err)
	}
	db.row = photoSizesRow{err: errors.New("query failed")}
	if _, _, err := repository.LoadSizes(context.Background(), 8); err == nil {
		t.Fatal("LoadSizes swallowed query error")
	}
}

type photoSizesDB struct {
	disconnectedDB
	row photoSizesRow
}

func (db *photoSizesDB) QueryRow(context.Context, string, ...interface{}) pgx.Row { return db.row }

type photoSizesRow struct {
	values []any
	err    error
}

func (r photoSizesRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch out := dest[i].(type) {
		case *int64:
			*out = r.values[i].(int64)
		case *time.Time:
			*out = r.values[i].(time.Time)
		case *string:
			*out = r.values[i].(string)
		case *pgtype.Timestamptz:
			*out = r.values[i].(pgtype.Timestamptz)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

// disconnectedDB permits constructor tests; guarded call paths return before it is used.
type disconnectedDB struct{}

func (disconnectedDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (disconnectedDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

func (disconnectedDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return nil
}

func (disconnectedDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}
