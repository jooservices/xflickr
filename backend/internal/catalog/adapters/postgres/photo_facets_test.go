package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestLoadPhotoFacetSource(t *testing.T) {
	updatedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	db := &photoFacetDBFake{row: photoFacetRow{values: []any{"secret", updatedAt, "confirmed", pgtype.Timestamptz{Time: updatedAt, Valid: true}}}}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	source, exists, err := repository.LoadPhotoFacetSource(context.Background(), 42)
	if err != nil || !exists || source.Secret != "secret" || !source.LastUpdate.Equal(updatedAt) || source.SizesStatus != "confirmed" || source.SizesCheckedAt == nil || !source.SizesCheckedAt.Equal(updatedAt) {
		t.Fatalf("LoadPhotoFacetSource()=%+v exists=%t err=%v", source, exists, err)
	}
	db.row = photoFacetRow{err: pgx.ErrNoRows}
	if _, exists, err := repository.LoadPhotoFacetSource(context.Background(), 42); err != nil || exists {
		t.Fatalf("LoadPhotoFacetSource(missing) exists=%t err=%v", exists, err)
	}
	db.row = photoFacetRow{err: errors.New("query failed")}
	if _, _, err := repository.LoadPhotoFacetSource(context.Background(), 42); err == nil {
		t.Fatal("LoadPhotoFacetSource swallowed query error")
	}
	db.row = photoFacetRow{values: []any{"secret", updatedAt, "confirmed", pgtype.Timestamptz{}}}
	if source, exists, err := repository.LoadPhotoFacetSource(context.Background(), 42); err != nil || !exists || source.SizesCheckedAt != nil {
		t.Fatalf("LoadPhotoFacetSource(without size check)=%+v exists=%t err=%v", source, exists, err)
	}
	if _, _, err := repository.LoadPhotoFacetSource(nil, 42); err == nil {
		t.Fatal("LoadPhotoFacetSource accepted nil context")
	}
	if _, _, err := repository.LoadPhotoFacetSource(context.Background(), 0); err == nil {
		t.Fatal("LoadPhotoFacetSource accepted invalid id")
	}
	var nilRepository *Repository
	if _, _, err := nilRepository.LoadPhotoFacetSource(context.Background(), 42); err == nil {
		t.Fatal("LoadPhotoFacetSource accepted nil repository")
	}
}

type photoFacetRow struct {
	values []any
	err    error
}

func (r photoFacetRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unexpected scan value count")
	}
	for index, value := range r.values {
		switch target := dest[index].(type) {
		case *string:
			*target = value.(string)
		case *time.Time:
			*target = value.(time.Time)
		case *pgtype.Timestamptz:
			*target = value.(pgtype.Timestamptz)
		default:
			return errors.New("unexpected scan destination")
		}
	}
	return nil
}

func TestSetPhotoFacetWritesEachShapeInATransaction(t *testing.T) {
	updatedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	fetchedAt := updatedAt.Add(time.Minute)
	tests := []struct {
		facet string
		data  string
	}{
		{facet: "info", data: `{"id":"42","title":"photo"}`},
		{facet: "exif", data: `{"id":"42","camera":"Fake Camera","exif":[]}`},
		{facet: "contexts", data: `{"set":[{"id":"set-1","title":"Album","url":"/sets/1"}],"pool":[{"id":"pool-1","title":"Pool","url":"/groups/1"}]}`},
		{facet: "contexts", data: `{"set":[],"pool":[]}`},
		{facet: "comments", data: `[{"id":"comment-1","author":"user","datecreate":"1700000000","permalink":"/comment/1","_content":"hello"}]`},
		{facet: "comments", data: `[]`},
		{facet: "photo_favorites", data: `[{"nsid":"user","username":"User","favedate":"1700000000"}]`},
		{facet: "photo_favorites", data: `[]`},
	}
	for _, test := range tests {
		t.Run(test.facet+"/"+test.data, func(t *testing.T) {
			db := &photoFacetDBFake{}
			repository, err := New(db)
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.SetPhotoFacet(context.Background(), 42, test.facet, []byte(test.data), updatedAt, fetchedAt); err != nil {
				t.Fatalf("SetPhotoFacet(): %v", err)
			}
			if db.beginCount != 1 || db.commitCount != 1 || db.execCount < 2 {
				t.Fatalf("transaction calls: begin=%d commit=%d exec=%d", db.beginCount, db.commitCount, db.execCount)
			}
		})
	}
}

func TestSetPhotoFacetSupportsDatabaseWithoutTransactions(t *testing.T) {
	db := &repositoryTestDB{batch: &repositoryTestBatch{}}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if err := repository.SetPhotoFacet(context.Background(), 42, "info", []byte(`{"title":"photo"}`), updatedAt, updatedAt.Add(time.Minute)); err != nil {
		t.Fatalf("SetPhotoFacet() without transaction support: %v", err)
	}
}

func TestSetPhotoFacetRollsBackFailuresAndValidatesData(t *testing.T) {
	updatedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	fetchedAt := updatedAt.Add(time.Minute)
	boom := errors.New("database unavailable")
	newRepository := func(db *photoFacetDBFake) *Repository {
		repository, err := New(db)
		if err != nil {
			t.Fatal(err)
		}
		return repository
	}
	for _, test := range []struct {
		name  string
		facet string
		data  string
	}{
		{name: "unsupported facet", facet: "unknown", data: `{}`},
		{name: "malformed JSON", facet: "info", data: `not-json`},
		{name: "invalid EXIF", facet: "exif", data: `[]`},
		{name: "context without ID", facet: "contexts", data: `{"set":[{"title":"missing id"}],"pool":[]}`},
		{name: "comment without ID", facet: "comments", data: `[{"author":"user"}]`},
		{name: "favorite without NSID", facet: "photo_favorites", data: `[{"username":"user"}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := &photoFacetDBFake{}
			err := newRepository(db).SetPhotoFacet(context.Background(), 42, test.facet, []byte(test.data), updatedAt, fetchedAt)
			if err == nil {
				t.Fatal("SetPhotoFacet accepted invalid input")
			}
			if test.name == "malformed JSON" {
				if db.beginCount != 0 {
					t.Fatalf("malformed JSON began a transaction: %+v", db)
				}
				return
			}
			if db.beginCount != 1 || db.rollbackCount != 1 || db.commitCount != 0 {
				t.Fatalf("failure did not roll back: %+v", db)
			}
		})
	}
	for _, test := range []struct {
		name      string
		db        *photoFacetDBFake
		facet     string
		data      string
		wantError error
	}{
		{name: "begin", db: &photoFacetDBFake{beginErr: boom}, facet: "info", data: `{}`, wantError: boom},
		{name: "facet write", db: &photoFacetDBFake{execErrAt: 1, execErr: boom}, facet: "info", data: `{}`, wantError: boom},
		{name: "status write", db: &photoFacetDBFake{execErrAt: 2, execErr: boom}, facet: "info", data: `{}`, wantError: boom},
		{name: "batch write", db: &photoFacetDBFake{batch: &repositoryTestBatch{errs: []error{boom}}}, facet: "comments", data: `[{"id":"c1"}]`, wantError: boom},
		{name: "commit", db: &photoFacetDBFake{commitErr: boom}, facet: "info", data: `{}`, wantError: boom},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := newRepository(test.db).SetPhotoFacet(context.Background(), 42, test.facet, []byte(test.data), updatedAt, fetchedAt)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("SetPhotoFacet() error=%v, want %v", err, test.wantError)
			}
			if test.name != "begin" && test.db.rollbackCount != 1 {
				t.Fatalf("failure rollback count=%d, want 1", test.db.rollbackCount)
			}
		})
	}
	for _, test := range []struct {
		name string
		id   int64
		data []byte
		at   time.Time
	}{
		{name: "nil context", id: 42, data: []byte(`{}`), at: updatedAt},
		{name: "invalid id", id: 0, data: []byte(`{}`), at: updatedAt},
		{name: "zero version", id: 42, data: []byte(`{}`), at: time.Time{}},
		{name: "zero fetch time", id: 42, data: []byte(`{}`), at: updatedAt},
		{name: "invalid JSON", id: 42, data: []byte(`{`), at: updatedAt},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.name == "nil context" {
				ctx = nil
			}
			fetched := fetchedAt
			if test.name == "zero fetch time" {
				fetched = time.Time{}
			}
			if err := newRepository(&photoFacetDBFake{}).SetPhotoFacet(ctx, test.id, "info", test.data, test.at, fetched); err == nil {
				t.Fatal("SetPhotoFacet accepted invalid arguments")
			}
		})
	}
	if got := unixTimeValue("1700000000"); got == nil || !got.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Fatalf("unixTimeValue(valid)=%v", got)
	}
	if got := unixTimeValue("invalid"); got != nil {
		t.Fatalf("unixTimeValue(invalid)=%v, want nil", got)
	}
}

type photoFacetDBFake struct {
	row           pgx.Row
	beginErr      error
	commitErr     error
	execErrAt     int
	execErr       error
	execCount     int
	beginCount    int
	commitCount   int
	rollbackCount int
	batch         pgx.BatchResults
}

func (d *photoFacetDBFake) Begin(context.Context) (pgx.Tx, error) {
	d.beginCount++
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return &photoFacetTx{db: d}, nil
}

func (d *photoFacetDBFake) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	d.execCount++
	if d.execErrAt == d.execCount {
		return pgconn.CommandTag{}, d.execErr
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (*photoFacetDBFake) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (d *photoFacetDBFake) QueryRow(context.Context, string, ...any) pgx.Row { return d.row }

func (d *photoFacetDBFake) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	if d.batch != nil {
		return d.batch
	}
	return &repositoryTestBatch{}
}

type photoFacetTx struct {
	pgx.Tx
	db *photoFacetDBFake
}

func (tx *photoFacetTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return tx.db.Exec(ctx, query, args...)
}

func (tx *photoFacetTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	return tx.db.Query(ctx, query, args...)
}

func (tx *photoFacetTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return tx.db.QueryRow(ctx, query, args...)
}

func (tx *photoFacetTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return tx.db.SendBatch(ctx, batch)
}

func (tx *photoFacetTx) Commit(context.Context) error {
	tx.db.commitCount++
	return tx.db.commitErr
}

func (tx *photoFacetTx) Rollback(context.Context) error {
	tx.db.rollbackCount++
	return nil
}
