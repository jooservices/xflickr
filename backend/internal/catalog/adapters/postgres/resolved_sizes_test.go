package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

type resolvedSizesTx struct {
	pgx.Tx
	fail           string
	calls, commits int
	rolled         bool
}

var errResolvedSizes = errors.New("transaction failure")

func (tx *resolvedSizesTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.calls++
	if tx.calls == 1 && !strings.Contains(sql, "UPDATE photos") {
		return pgconn.CommandTag{}, errors.New("missing sizes update")
	}
	if tx.calls == 2 && (!strings.Contains(sql, "INSERT INTO outbox_events") || args[0] != app.SizesResolvedTopic) {
		return pgconn.CommandTag{}, errors.New("missing event")
	}
	if (tx.fail == "update" && tx.calls == 1) || (tx.fail == "event" && tx.calls == 2) {
		return pgconn.CommandTag{}, errResolvedSizes
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *resolvedSizesTx) Commit(context.Context) error {
	tx.commits++
	if tx.fail == "commit" {
		return errResolvedSizes
	}
	return nil
}
func (tx *resolvedSizesTx) Rollback(context.Context) error { tx.rolled = true; return nil }

type resolvedSizesPool struct {
	tx  *resolvedSizesTx
	err error
}

func (p resolvedSizesPool) Begin(context.Context) (pgx.Tx, error) { return p.tx, p.err }
func TestResolvedSizesPublishesAtomically(t *testing.T) {
	for _, status := range []domain.SizesStatus{domain.SizesConfirmed, domain.SizesUnavailable, domain.SizesProvisional} {
		tx := &resolvedSizesTx{}
		err := (ResolvedSizes{Pool: resolvedSizesPool{tx: tx}, Publish: testPublishResolvedSizes}).SetSizes(t.Context(), 42, []byte(`[]`), status, time.Now())
		want := 2
		if status == domain.SizesProvisional {
			want = 1
		}
		if err != nil || tx.calls != want || tx.commits != 1 || !tx.rolled {
			t.Fatalf("tx=%+v err=%v", tx, err)
		}
	}
	for _, fail := range []string{"begin", "update", "event", "commit", "invalid"} {
		t.Run(fail, func(t *testing.T) {
			tx := &resolvedSizesTx{fail: fail}
			pool := resolvedSizesPool{tx: tx}
			if fail == "begin" {
				pool.err = errResolvedSizes
			}
			id := int64(42)
			if fail == "invalid" {
				id = 0
			}
			err := (ResolvedSizes{Pool: pool, Publish: testPublishResolvedSizes}).SetSizes(t.Context(), id, []byte(`[]`), domain.SizesConfirmed, time.Now())
			if err == nil {
				t.Fatal("failure lost")
			}
			if fail != "invalid" && !errors.Is(err, errResolvedSizes) {
				t.Fatal(err)
			}
			if fail != "commit" && tx.commits != 0 {
				t.Fatal("partial result committed")
			}
		})
	}
	if (ResolvedSizes{}).SetSizes(t.Context(), 1, nil, domain.SizesConfirmed, time.Now()) == nil {
		t.Fatal("nil pool accepted")
	}
	if (ResolvedSizes{Pool: resolvedSizesPool{}}).SetSizes(nil, 1, nil, domain.SizesConfirmed, time.Now()) == nil {
		t.Fatal("nil context accepted")
	}
}

func testPublishResolvedSizes(ctx context.Context, tx pgx.Tx, event app.SizesResolved) error {
	_, err := tx.Exec(ctx, "INSERT INTO outbox_events", app.SizesResolvedTopic, event)
	return err
}
