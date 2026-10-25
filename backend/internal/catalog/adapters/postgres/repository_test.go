package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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
