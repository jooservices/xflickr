package gen

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeDB struct {
	batch *fakeBatch
	err   error
	calls int
}

func (d *fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	d.calls++
	return pgconn.CommandTag{}, d.err
}
func (*fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (*fakeDB) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
func (d *fakeDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return d.batch
}

type fakeBatch struct {
	errs   []error
	called int
	closed bool
}

func (b *fakeBatch) Exec() (pgconn.CommandTag, error) {
	index := b.called
	b.called++
	if index < len(b.errs) {
		return pgconn.CommandTag{}, b.errs[index]
	}
	return pgconn.CommandTag{}, nil
}
func (*fakeBatch) Query() (pgx.Rows, error) { return nil, nil }
func (*fakeBatch) QueryRow() pgx.Row        { return nil }
func (b *fakeBatch) Close() error           { b.closed = true; return nil }

func TestCatalogQueriesAndGeneratedBatchResults(t *testing.T) {
	ctx := context.Background()
	db := &fakeDB{batch: &fakeBatch{}}
	queries := New(db)
	if err := queries.SetSizes(ctx, SetSizesParams{ID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := queries.MarkTombstone(ctx, MarkTombstoneParams{ID: 1}); err != nil {
		t.Fatal(err)
	}
	person := EnsurePeopleParams{Nsids: []string{"person"}, Usernames: []string{"person"}}
	if err := queries.EnsurePeople(ctx, person); err != nil {
		t.Fatalf("EnsurePeople error = %v", err)
	}
	if err := queries.RefreshChangedPeople(ctx, RefreshChangedPeopleParams{Nsids: person.Nsids, Usernames: person.Usernames}); err != nil {
		t.Fatalf("RefreshChangedPeople error = %v", err)
	}
	db.batch = &fakeBatch{errs: []error{errors.New("write failed")}}
	photoFailureResults := queries.UpsertPhoto(ctx, []UpsertPhotoParams{{ID: 1}})
	var photoErr error
	photoFailureResults.Exec(func(_ int, err error) { photoErr = err })
	if photoErr == nil {
		t.Fatal("photo batch error was not reported")
	}
	db.batch = &fakeBatch{}
	photoResults := queries.UpsertPhoto(ctx, []UpsertPhotoParams{{ID: 2}})
	photoResults.Exec(nil)
	if err := New(&fakeDB{err: errors.New("exec failed")}).SetSizes(ctx, SetSizesParams{}); err == nil {
		t.Fatal("SetSizes error was lost")
	}
	if err := New(&fakeDB{err: errors.New("exec failed")}).MarkTombstone(ctx, MarkTombstoneParams{}); err == nil {
		t.Fatal("MarkTombstone error was lost")
	}
	if err := New(&fakeDB{err: errors.New("exec failed")}).EnsurePeople(ctx, person); err == nil {
		t.Fatal("EnsurePeople error was lost")
	}
	if err := New(&fakeDB{err: errors.New("exec failed")}).RefreshChangedPeople(ctx, RefreshChangedPeopleParams{Nsids: person.Nsids}); err == nil {
		t.Fatal("RefreshChangedPeople error was lost")
	}
	if New(db).WithTx(nil).db != nil {
		t.Fatal("WithTx did not retain supplied transaction")
	}
}

func TestRiverJobStateScanners(t *testing.T) {
	var state RiverJobState
	if err := state.Scan([]byte("available")); err != nil || state != RiverJobStateAvailable {
		t.Fatalf("scan bytes: state=%q err=%v", state, err)
	}
	if err := state.Scan("completed"); err != nil || state != RiverJobStateCompleted {
		t.Fatalf("scan string: state=%q err=%v", state, err)
	}
	if err := state.Scan(1); err == nil {
		t.Fatal("unsupported scan succeeded")
	}
	var nullable NullRiverJobState
	if err := nullable.Scan(nil); err != nil || nullable.Valid {
		t.Fatalf("scan NULL: nullable=%+v err=%v", nullable, err)
	}
	if value, err := (NullRiverJobState{}).Value(); err != nil || value != nil {
		t.Fatalf("invalid Value() = %v, %v", value, err)
	}
	if err := nullable.Scan("running"); err != nil || !nullable.Valid || nullable.RiverJobState != "running" {
		t.Fatalf("scan valid state: nullable=%+v err=%v", nullable, err)
	}
	if value, err := nullable.Value(); err != nil || value != "running" {
		t.Fatalf("valid Value() = %v, %v", value, err)
	}
}
