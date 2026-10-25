package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
)

type repositoryTestDB struct {
	batch   *repositoryTestBatch
	execErr error
}

func (d *repositoryTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, d.execErr
}

func (*repositoryTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (*repositoryTestDB) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
func (d *repositoryTestDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return d.batch
}

type repositoryTestBatch struct {
	errs  []error
	index int
}

func (b *repositoryTestBatch) Exec() (pgconn.CommandTag, error) {
	index := b.index
	b.index++
	if index < len(b.errs) {
		return pgconn.CommandTag{}, b.errs[index]
	}
	return pgconn.CommandTag{}, nil
}

func (*repositoryTestBatch) Query() (pgx.Rows, error) { return nil, nil }
func (*repositoryTestBatch) QueryRow() pgx.Row        { return nil }
func (*repositoryTestBatch) Close() error             { return nil }

func TestRepositoryBatchWritesAndFailures(t *testing.T) {
	ctx := context.Background()
	db := &repositoryTestDB{batch: &repositoryTestBatch{}}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(ctx, []catalog.Person{{NSID: "owner", Username: "Owner"}}); err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	if err := repository.UpsertPhotos(ctx, []catalog.Photo{{
		ID: 1, OwnerNSID: "owner", LastUpdate: time.Unix(1700000000, 0),
		Visibility: catalog.VisibilityPublic, SizesStatus: catalog.SizesProvisional,
		Sizes: []byte(`[]`), Raw: []byte(`{"id":1}`),
	}}); err != nil {
		t.Fatalf("upsert photo: %v", err)
	}

	batchErr := errors.New("batch write failed")
	db.batch = &repositoryTestBatch{errs: []error{batchErr}}
	if err := repository.UpsertPeople(ctx, []catalog.Person{{NSID: "owner", Username: "Owner"}}); !errors.Is(err, batchErr) {
		t.Fatalf("person batch error = %v", err)
	}
	db.batch = &repositoryTestBatch{errs: []error{batchErr}}
	if err := repository.UpsertPhotos(ctx, []catalog.Photo{{
		ID: 2, OwnerNSID: "owner", LastUpdate: time.Unix(1700000000, 0),
		Visibility: catalog.VisibilityPublic, Raw: []byte(`{"id":2}`),
	}}); !errors.Is(err, batchErr) {
		t.Fatalf("photo batch error = %v", err)
	}

	checkedAt := time.Unix(1700000100, 0)
	if err := repository.SetSizes(ctx, 1, []byte(`[]`), catalog.SizesConfirmed, checkedAt); err != nil {
		t.Fatalf("set sizes: %v", err)
	}
	if err := repository.MarkTombstone(ctx, 1, checkedAt); err != nil {
		t.Fatalf("mark tombstone: %v", err)
	}
	db.execErr = errors.New("statement failed")
	if err := repository.SetSizes(ctx, 1, []byte(`[]`), catalog.SizesConfirmed, checkedAt); !errors.Is(err, db.execErr) {
		t.Fatalf("set sizes statement error = %v", err)
	}
	if err := repository.MarkTombstone(ctx, 1, checkedAt); !errors.Is(err, db.execErr) {
		t.Fatalf("tombstone statement error = %v", err)
	}
}

func TestRepositoryValidatesBeforeWriting(t *testing.T) {
	repository, err := New(&repositoryTestDB{batch: &repositoryTestBatch{}})
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
	if err := repository.UpsertPeople(ctx, []catalog.Person{{NSID: "owner"}}); err == nil {
		t.Fatal("person without username succeeded")
	}
	if err := repository.UpsertPhotos(ctx, []catalog.Photo{{ID: 1, OwnerNSID: "owner", LastUpdate: time.Now(), Visibility: "invalid", Raw: []byte(`{}`)}}); err == nil {
		t.Fatal("invalid photo succeeded")
	}
	if err := repository.SetSizes(ctx, 0, []byte(`[]`), catalog.SizesConfirmed, time.Now()); err == nil {
		t.Fatal("zero photo id succeeded")
	}
	if err := repository.SetSizes(ctx, 1, []byte(`bad`), catalog.SizesConfirmed, time.Now()); err == nil {
		t.Fatal("invalid sizes JSON succeeded")
	}
	if err := repository.MarkTombstone(ctx, 0, time.Now()); err == nil {
		t.Fatal("zero photo id tombstone succeeded")
	}
}
