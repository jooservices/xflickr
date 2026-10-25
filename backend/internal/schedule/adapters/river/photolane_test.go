package river

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/platform/queue"

	"github.com/jooservices/xflickr/internal/schedule/app"
)

type failingBegin struct{}

func (failingBegin) Begin(context.Context) (pgx.Tx, error) { return nil, errors.New("begin failed") }

func TestPhotoEnqueuerValidatesInput(t *testing.T) {
	job := app.PhotoJob{PhotoID: 1, KeyID: 2, DedupeKey: "photo:1:sizes"}
	if _, err := (PhotoEnqueuer{}).EnqueuePhoto(t.Context(), job); err == nil {
		t.Fatal("EnqueuePhoto() without dependencies succeeded")
	}
	enqueuer := PhotoEnqueuer{Pool: failingBegin{}, Jobs: queueFake{}}
	for _, bad := range []app.PhotoJob{{KeyID: 2, DedupeKey: "k"}, {PhotoID: 1, DedupeKey: "k"}, {PhotoID: 1, KeyID: 2}} {
		if _, err := enqueuer.EnqueuePhoto(t.Context(), bad); err == nil {
			t.Fatalf("EnqueuePhoto(%+v) succeeded", bad)
		}
	}
	if _, err := enqueuer.EnqueuePhoto(t.Context(), job); err == nil || err.Error() != "schedule: begin photo enqueue: begin failed" {
		t.Fatalf("EnqueuePhoto() error = %v, want begin failure", err)
	}
}

type photoTx struct {
	pgx.Tx
	execErr, scanErr, commitErr error
	queued                      bool
	committed                   bool
}

func (t *photoTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, t.execErr
}
func (t *photoTx) QueryRow(context.Context, string, ...any) pgx.Row { return photoRow{t} }
func (t *photoTx) Commit(context.Context) error                     { t.committed = true; return t.commitErr }
func (t *photoTx) Rollback(context.Context) error                   { return nil }

type photoRow struct{ tx *photoTx }

func (r photoRow) Scan(dest ...any) error {
	*dest[0].(*bool) = r.tx.queued
	return r.tx.scanErr
}

type txBegin struct{ tx *photoTx }

func (b txBegin) Begin(context.Context) (pgx.Tx, error) { return b.tx, nil }

type countingJobs struct {
	queueFake
	inserted int
	err      error
}

func (j *countingJobs) Enqueue(context.Context, pgx.Tx, queue.Job, queue.Options) error {
	j.inserted++
	return j.err
}

func TestPhotoEnqueuerTransactionOutcomes(t *testing.T) {
	boom := errors.New("boom")
	job := app.PhotoJob{PhotoID: 1, KeyID: 2, DedupeKey: "photo:1:sizes"}
	tests := []struct {
		name         string
		tx           *photoTx
		jobsErr      error
		wantOK       bool
		wantErr      error
		wantInserted int
	}{
		{name: "inserted", tx: &photoTx{}, wantOK: true, wantInserted: 1},
		{name: "already queued", tx: &photoTx{queued: true}},
		{name: "lock fails", tx: &photoTx{execErr: boom}, wantErr: boom},
		{name: "check fails", tx: &photoTx{scanErr: boom}, wantErr: boom},
		{name: "insert fails", tx: &photoTx{}, jobsErr: boom, wantErr: boom, wantInserted: 1},
		{name: "commit fails", tx: &photoTx{commitErr: boom}, wantErr: boom, wantInserted: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := &countingJobs{err: tt.jobsErr}
			ok, err := PhotoEnqueuer{Pool: txBegin{tt.tx}, Jobs: jobs}.EnqueuePhoto(t.Context(), job)
			if ok != tt.wantOK || !errors.Is(err, tt.wantErr) || jobs.inserted != tt.wantInserted {
				t.Fatalf("EnqueuePhoto() = %v, %v with %d inserts", ok, err, jobs.inserted)
			}
		})
	}
}
