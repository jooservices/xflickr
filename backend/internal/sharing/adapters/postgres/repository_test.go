package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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

func TestNewAndSetValidation(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected missing pool error")
	}
	if _, err := New(beginnerStub{}); err != nil {
		t.Fatalf("New() error = %v", err)
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
	wantErr := errors.New("transaction")
	if _, err := (&Repository{run: func(context.Context, func(settingTx) error) error { return wantErr }}).Set(context.Background(), domain.Setting{UserID: 1}); !errors.Is(err, wantErr) {
		t.Fatalf("transaction error = %v", err)
	}
}
