package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	homeapp "github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/sharing/domain"
)

type rowStub struct {
	values []any
	err    error
}

func (r rowStub) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch out := dest[i].(type) {
		case *int64:
			*out = r.values[i].(int64)
		case *bool:
			*out = r.values[i].(bool)
		case *time.Time:
			*out = r.values[i].(time.Time)
		}
	}
	return nil
}

type txStub struct {
	insert    rowStub
	selectRow rowStub
	execErr   error
	execs     *int
}

type beginnerStub struct{}

func (beginnerStub) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return nil, nil }

func (t txStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if strings.HasPrefix(query, "INSERT") {
		return t.insert
	}
	return t.selectRow
}

func (t txStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if t.execs != nil {
		*t.execs++
	}
	return pgconn.NewCommandTag("INSERT 0 1"), t.execErr
}

type queueTxStub struct {
	pgx.Tx
	txStub
}

func (tx queueTxStub) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return tx.txStub.QueryRow(ctx, query, args...)
}

func (tx queueTxStub) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return tx.txStub.Exec(ctx, query, args...)
}

type refreshQueueStub struct {
	job     queue.Job
	options queue.Options
	err     error
	calls   int
}

func (q *refreshQueueStub) Enqueue(_ context.Context, _ pgx.Tx, job queue.Job, options queue.Options) error {
	q.job = job
	q.options = options
	q.calls++
	return q.err
}

func (*refreshQueueStub) Register(string, queue.Handler) error { return nil }

func TestSetWithTransactionFake(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	values := []any{int64(7), true, now}
	for _, tc := range []struct {
		name        string
		tx          txStub
		wantErr     bool
		wantPublish bool
	}{
		{name: "changed", tx: txStub{insert: rowStub{values: values}}, wantPublish: true},
		{name: "unchanged", tx: txStub{insert: rowStub{err: pgx.ErrNoRows}, selectRow: rowStub{values: values}}},
		{name: "insert failure", tx: txStub{insert: rowStub{err: errors.New("query")}}, wantErr: true},
		{name: "missing existing row", tx: txStub{insert: rowStub{err: pgx.ErrNoRows}, selectRow: rowStub{err: pgx.ErrNoRows}}, wantErr: true},
		{name: "publish failure", tx: txStub{insert: rowStub{values: values}, execErr: errors.New("outbox")}, wantErr: true, wantPublish: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execs := 0
			tc.tx.execs = &execs
			repo := &Repository{run: func(_ context.Context, fn func(settingTx) error) error { return fn(tc.tx) }}
			got, err := repo.Set(context.Background(), domain.Setting{UserID: 7, Enabled: true})
			if (err != nil) != tc.wantErr {
				t.Fatalf("Set() error = %v", err)
			}
			if (execs > 0) != tc.wantPublish {
				t.Fatalf("outbox publishes = %d, want publish %v", execs, tc.wantPublish)
			}
			if !tc.wantErr && (got.UserID != 7 || !got.Enabled || !got.ChangedAt.Equal(now)) {
				t.Fatalf("setting = %+v", got)
			}
		})
	}
}

func TestGetDefaultAndStoredSettings(t *testing.T) {
	now := time.Unix(30, 0).UTC()
	for _, tc := range []struct {
		name    string
		row     rowStub
		want    domain.Setting
		wantErr bool
	}{
		{name: "default off", row: rowStub{err: pgx.ErrNoRows}, want: domain.Setting{UserID: 7}},
		{name: "stored choice", row: rowStub{values: []any{int64(7), true, now}}, want: domain.Setting{UserID: 7, Enabled: true, ChangedAt: now}},
		{name: "query error", row: rowStub{err: errors.New("query")}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &Repository{run: func(_ context.Context, fn func(settingTx) error) error {
				return fn(txStub{selectRow: tc.row})
			}}
			got, err := repo.Get(context.Background(), 7)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Get() error = %v", err)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("setting = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSetEnqueuesHomeRefreshInChangeTransaction(t *testing.T) {
	queueErr := errors.New("queue unavailable")
	for _, test := range []struct {
		name     string
		queueErr error
		wantErr  bool
	}{
		{name: "enqueued", wantErr: false},
		{name: "enqueue failure rolls back setting", queueErr: queueErr, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(20, 0).UTC()
			transaction := queueTxStub{txStub: txStub{insert: rowStub{values: []any{int64(7), true, now}}}}
			jobs := &refreshQueueStub{err: test.queueErr}
			repository := &Repository{
				run: func(_ context.Context, fn func(settingTx) error) error {
					return fn(transaction)
				},
				queue: jobs,
			}
			_, err := repository.Set(t.Context(), domain.Setting{UserID: 7, Enabled: true})
			if (err != nil) != test.wantErr || test.wantErr && !errors.Is(err, queueErr) {
				t.Fatalf("Set() error = %v, wantErr %t", err, test.wantErr)
			}
			if jobs.calls != 1 || jobs.job.Kind != homeapp.RefreshJobKind || jobs.options.Queue != "home" || jobs.options.UniqueByArgs || jobs.options.UniqueByQueue {
				t.Fatalf("queued Home refresh = %+v, options=%+v", jobs.job, jobs.options)
			}
			var args homeapp.RefreshJob
			if err := json.Unmarshal(jobs.job.Args, &args); err != nil || args.UserID != 7 {
				t.Fatalf("Home refresh args = %+v, err=%v", args, err)
			}
		})
	}
}

func TestNewAndSetValidation(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected missing pool error")
	}
	if _, err := New(beginnerStub{}); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := New(beginnerStub{}, nil); err == nil {
		t.Fatal("New() accepted a nil refresh queue")
	}
	if _, err := New(beginnerStub{}, &refreshQueueStub{}, &refreshQueueStub{}); err == nil {
		t.Fatal("New() accepted multiple refresh queues")
	}
	refreshQueue := &refreshQueueStub{}
	withRefreshQueue, err := New(beginnerStub{}, refreshQueue)
	if err != nil || withRefreshQueue.queue != refreshQueue {
		t.Fatalf("New() refresh queue = %v, err=%v", withRefreshQueue, err)
	}
	for _, repo := range []*Repository{nil, {}, {run: func(context.Context, func(settingTx) error) error { return nil }}} {
		if repo != nil && repo.run != nil {
			continue
		}
		if _, err := repo.Set(context.Background(), domain.Setting{UserID: 1}); err == nil {
			t.Fatal("expected incomplete repository error")
		}
	}
	if _, err := (&Repository{run: func(context.Context, func(settingTx) error) error { return nil }}).Set(nil, domain.Setting{UserID: 1}); err == nil {
		t.Fatal("expected missing context error")
	}
	if _, err := (&Repository{run: func(context.Context, func(settingTx) error) error { return nil }}).Set(context.Background(), domain.Setting{}); err == nil {
		t.Fatal("expected missing user error")
	}
	if _, err := (&Repository{run: func(context.Context, func(settingTx) error) error { return nil }}).Get(context.Background(), 0); err == nil {
		t.Fatal("expected missing reader user error")
	}
	wantErr := errors.New("transaction")
	if _, err := (&Repository{run: func(context.Context, func(settingTx) error) error { return wantErr }}).Set(context.Background(), domain.Setting{UserID: 1}); !errors.Is(err, wantErr) {
		t.Fatalf("transaction error = %v", err)
	}
	if _, err := (&Repository{run: func(context.Context, func(settingTx) error) error { return wantErr }}).Get(context.Background(), 1); !errors.Is(err, wantErr) {
		t.Fatalf("read transaction error = %v", err)
	}
	if _, err := (&Repository{run: func(context.Context, func(settingTx) error) error { return nil }}).Get(nil, 1); err == nil {
		t.Fatal("expected missing reader context error")
	}
}
