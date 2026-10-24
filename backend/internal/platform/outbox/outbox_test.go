package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPublishInsertsJSONPayload(t *testing.T) {
	tx := &fakeOutboxTx{}
	err := Publish(tx, "photo.updated", "photo-17", map[string]any{"id": 17})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if !strings.Contains(tx.execSQL, "INSERT INTO outbox_events") || len(tx.execArgs) != 3 {
		t.Fatalf("Exec() query/args = %q/%#v", tx.execSQL, tx.execArgs)
	}
	if tx.execArgs[0] != "photo.updated" || tx.execArgs[1] != "photo-17" {
		t.Fatalf("topic/key = %#v/%#v", tx.execArgs[0], tx.execArgs[1])
	}
	var payload map[string]any
	if err := json.Unmarshal(tx.execArgs[2].([]byte), &payload); err != nil || payload["id"] != float64(17) {
		t.Fatalf("encoded payload = %#v, unmarshal error = %v", tx.execArgs[2], err)
	}
}

func TestPublishValidationAndFailures(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		tx      Tx
		topic   string
		payload any
		wantErr string
	}{
		{name: "nil context", tx: &fakeOutboxTx{}, topic: "topic", wantErr: "context is required"},
		{name: "nil transaction", ctx: t.Context(), topic: "topic", wantErr: "transaction is required"},
		{name: "empty topic", ctx: t.Context(), tx: &fakeOutboxTx{}, topic: "  ", wantErr: "topic is required"},
		{name: "unencodable payload", ctx: t.Context(), tx: &fakeOutboxTx{}, topic: "topic", payload: make(chan int), wantErr: "encode event payload"},
		{name: "insert failure", ctx: t.Context(), tx: &fakeOutboxTx{execErr: errors.New("insert failed")}, topic: "topic", wantErr: "insert event"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := PublishContext(test.ctx, test.tx, test.topic, "key", test.payload)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Publish() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestRetryPolicyExponentialBackoffAndBounds(t *testing.T) {
	policy := DefaultRetryPolicy()
	for attempt, want := range map[int]time.Duration{
		0:    0,
		1:    100 * time.Millisecond,
		2:    200 * time.Millisecond,
		3:    400 * time.Millisecond,
		7:    5 * time.Second,
		1000: 5 * time.Second,
	} {
		if got := policy.Delay(attempt); got != want {
			t.Errorf("Delay(%d) = %s, want %s", attempt, got, want)
		}
	}
	if got := (RetryPolicy{InitialDelay: 0, MaxDelay: 0}).Delay(1); got != defaultRetryDelay {
		t.Errorf("Delay() with invalid values = %s, want default %s", got, defaultRetryDelay)
	}
	if got := (RetryPolicy{InitialDelay: time.Second, MaxDelay: time.Millisecond}).Delay(3); got != time.Second {
		t.Errorf("Delay() with max below initial = %s, want one second", got)
	}
}

func TestConsumeHandlesEventAndMarksIt(t *testing.T) {
	event := testOutboxEvent()
	tx := &fakeOutboxTx{row: fakeRow{event: event}}
	database := &fakeConsumerDB{transactions: []pgx.Tx{tx}}
	consumer := NewConsumer(database)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var handled Event
	err := consumer.Consume(ctx, event.Topic, func(_ context.Context, got Event) error {
		handled = got
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume() error = %v, want context canceled", err)
	}
	if handled.ID != event.ID || handled.Key != event.Key || string(handled.Payload) != string(event.Payload) {
		t.Fatalf("handler received event %#v, want %#v", handled, event)
	}
	if tx.execCalls != 1 || tx.commits != 1 || database.options[0].IsoLevel != pgx.ReadCommitted {
		t.Fatalf("exec=%d commits=%d options=%+v", tx.execCalls, tx.commits, database.options)
	}
}

func TestConsumeRetriesHandlerFailureWithBackoff(t *testing.T) {
	event := testOutboxEvent()
	first := &fakeOutboxTx{row: fakeRow{event: event}}
	second := &fakeOutboxTx{row: fakeRow{event: event}}
	database := &fakeConsumerDB{transactions: []pgx.Tx{first, second}}
	consumer := NewConsumerWithRetryPolicy(database, RetryPolicy{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	err := consumer.Consume(ctx, event.Topic, func(_ context.Context, _ Event) error {
		calls++
		if calls == 1 {
			return errors.New("temporary handler failure")
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatalf("Consume() error/calls = %v/%d", err, calls)
	}
	if first.rollbacks != 1 || second.commits != 1 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestConsumeRetriesDatabaseAndMarkFailures(t *testing.T) {
	event := testOutboxEvent()
	tests := []struct {
		name  string
		first pgx.Tx
		begin error
	}{
		{name: "begin", begin: errors.New("database unavailable")},
		{name: "read", first: &fakeOutboxTx{row: fakeRow{err: errors.New("read failed")}}},
		{name: "mark", first: &fakeOutboxTx{row: fakeRow{event: event}, execErr: errors.New("update failed")}},
		{name: "commit", first: &fakeOutboxTx{row: fakeRow{event: event}, commitErr: errors.New("commit failed")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			second := &fakeOutboxTx{row: fakeRow{event: event}}
			transactions := []pgx.Tx{second}
			beginErrors := []error{test.begin}
			if test.first != nil {
				transactions = []pgx.Tx{test.first, second}
				beginErrors = []error{nil, nil}
			}
			database := &fakeConsumerDB{transactions: transactions, beginErrors: beginErrors}
			consumer := NewConsumerWithRetryPolicy(database, RetryPolicy{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			wantCalls := 1
			if test.name == "mark" || test.name == "commit" {
				wantCalls = 2
			}
			err := consumer.Consume(ctx, event.Topic, func(_ context.Context, _ Event) error {
				calls++
				if calls == wantCalls {
					cancel()
				}
				return nil
			})
			if !errors.Is(err, context.Canceled) || calls != wantCalls {
				t.Fatalf("Consume() error/callback calls = %v/%d, want %d", err, calls, wantCalls)
			}
			if database.begins != 1+boolToInt(test.first != nil || test.begin != nil) {
				t.Fatalf("BeginTx calls = %d", database.begins)
			}
		})
	}
}

func TestConsumePollsWhenNoEventAndReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	tx := &fakeOutboxTx{row: fakeRow{err: pgx.ErrNoRows}}
	database := &fakeConsumerDB{
		transactions: []pgx.Tx{tx},
		onBegin:      cancel,
	}
	err := NewConsumer(database).Consume(ctx, "topic", func(context.Context, Event) error {
		t.Fatal("handler called without an event")
		return nil
	})
	if !errors.Is(err, context.Canceled) || tx.commits != 0 || tx.rollbacks == 0 {
		t.Fatalf("Consume() error/tx = %v/%+v", err, tx)
	}
}

func TestConsumeValidationAndWait(t *testing.T) {
	validHandler := func(context.Context, Event) error { return nil }
	if err := (*Consumer)(nil).Consume(t.Context(), "topic", validHandler); err == nil {
		t.Fatal("Consume() with nil consumer succeeded")
	}
	if err := NewConsumer(nil).Consume(t.Context(), "topic", validHandler); err == nil {
		t.Fatal("Consume() with nil database succeeded")
	}
	if err := NewConsumer(&fakeConsumerDB{}).Consume(nil, "topic", validHandler); err == nil {
		t.Fatal("Consume() with nil context succeeded")
	}
	if err := NewConsumer(&fakeConsumerDB{}).Consume(t.Context(), "  ", validHandler); err == nil {
		t.Fatal("Consume() with empty topic succeeded")
	}
	if err := NewConsumer(&fakeConsumerDB{}).Consume(t.Context(), "topic", nil); err == nil {
		t.Fatal("Consume() with nil handler succeeded")
	}
	if err := wait(t.Context(), 0); err != nil {
		t.Fatalf("wait() with no delay error = %v", err)
	}
}

func testOutboxEvent() Event {
	return Event{
		ID:        42,
		Topic:     "photo.updated",
		Key:       "photo-42",
		Payload:   json.RawMessage(`{"id":42}`),
		CreatedAt: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC),
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type fakeOutboxTx struct {
	pgx.Tx
	row       pgx.Row
	execSQL   string
	execArgs  []any
	execErr   error
	commitErr error
	execCalls int
	commits   int
	rollbacks int
}

func (tx *fakeOutboxTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	tx.execSQL = query
	tx.execArgs = args
	return pgconn.CommandTag{}, tx.execErr
}

func (tx *fakeOutboxTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.row == nil {
		return fakeRow{err: pgx.ErrNoRows}
	}
	return tx.row
}

func (tx *fakeOutboxTx) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

func (tx *fakeOutboxTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type fakeRow struct {
	event Event
	err   error
}

func (row fakeRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*int64) = row.event.ID
	*dest[1].(*string) = row.event.Topic
	*dest[2].(*string) = row.event.Key
	*dest[3].(*json.RawMessage) = row.event.Payload
	*dest[4].(*time.Time) = row.event.CreatedAt
	return nil
}

type fakeConsumerDB struct {
	transactions    []pgx.Tx
	beginErrors     []error
	options         []pgx.TxOptions
	onBegin         func()
	begins          int
	successfulBegin int
}

func (database *fakeConsumerDB) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	database.begins++
	database.options = append(database.options, options)
	if database.onBegin != nil {
		database.onBegin()
	}
	if database.begins <= len(database.beginErrors) && database.beginErrors[database.begins-1] != nil {
		return nil, database.beginErrors[database.begins-1]
	}
	if database.successfulBegin >= len(database.transactions) {
		return nil, errors.New("unexpected transaction attempt")
	}
	tx := database.transactions[database.successfulBegin]
	database.successfulBegin++
	return tx, nil
}
