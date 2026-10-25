package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/schedule/domain"
)

func TestEnqueue(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		tx      pgx.Tx
		item    domain.WorkItem
		wantErr bool
	}{
		{name: "nil context", tx: &backlogFakeTx{}, item: validItem(), wantErr: true},
		{name: "nil transaction", ctx: context.Background(), item: validItem(), wantErr: true},
		{name: "invalid item", ctx: context.Background(), tx: &backlogFakeTx{}, item: domain.WorkItem{}, wantErr: true},
		{name: "cannot enqueue ready state", ctx: context.Background(), tx: &backlogFakeTx{}, item: domain.WorkItem{Kind: "person", SubjectKey: "a", State: domain.StateReady}, wantErr: true},
		{name: "success", ctx: context.Background(), tx: &backlogFakeTx{}, item: validItem()},
		{name: "database failure", ctx: context.Background(), tx: &backlogFakeTx{err: errors.New("db error")}, item: validItem(), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Enqueue(test.ctx, test.tx, test.item)
			if (err != nil) != test.wantErr {
				t.Fatalf("Enqueue() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.name == "success" {
				fake := test.tx.(*backlogFakeTx)
				if !strings.Contains(fake.query, "ON CONFLICT ON CONSTRAINT work_items_dedupe_key") || len(fake.args) != 14 {
					t.Fatalf("enqueue SQL/args not as expected: %q %#v", fake.query, fake.args)
				}
			}
		})
	}
}

func TestAdvance(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		tx      pgx.Tx
		id      int64
		cursor  json.RawMessage
		tag     pgconn.CommandTag
		txErr   error
		wantErr bool
	}{
		{name: "nil context", tx: &backlogFakeTx{}, id: 1, cursor: json.RawMessage(`{"page":2}`), wantErr: true},
		{name: "nil transaction", ctx: context.Background(), id: 1, cursor: json.RawMessage(`{}`), wantErr: true},
		{name: "invalid id", ctx: context.Background(), tx: &backlogFakeTx{}, cursor: json.RawMessage(`{}`), wantErr: true},
		{name: "invalid cursor", ctx: context.Background(), tx: &backlogFakeTx{}, id: 1, cursor: json.RawMessage("{"), wantErr: true},
		{name: "not running", ctx: context.Background(), tx: &backlogFakeTx{}, id: 1, cursor: json.RawMessage(`{}`), wantErr: true},
		{name: "success", ctx: context.Background(), tx: &backlogFakeTx{tag: pgconn.NewCommandTag("UPDATE 1")}, id: 1, cursor: json.RawMessage(`{"page":2}`)},
		{name: "database failure", ctx: context.Background(), tx: &backlogFakeTx{err: errors.New("db error")}, id: 1, cursor: json.RawMessage(`{}`), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Advance(test.ctx, test.tx, test.id, test.cursor)
			if (err != nil) != test.wantErr {
				t.Fatalf("Advance() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.name == "success" {
				fake := test.tx.(*backlogFakeTx)
				if !strings.Contains(fake.query, "state = 'pending'") || len(fake.args) != 2 {
					t.Fatalf("advance SQL/args not as expected: %q %#v", fake.query, fake.args)
				}
			}
		})
	}
}

func validItem() domain.WorkItem {
	return domain.WorkItem{Kind: "person", SubjectKey: "alice", PriorityBase: 10}
}

type backlogFakeTx struct {
	pgx.Tx
	query string
	args  []any
	tag   pgconn.CommandTag
	err   error
}

func (tx *backlogFakeTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.query, tx.args = query, args
	return tx.tag, tx.err
}

func (tx *backlogFakeTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
