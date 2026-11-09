package postgres

import (
	"context"
	"errors"
	"strconv"
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

type accountResolvedTx struct {
	pgx.Tx
	fail           string
	rows           int64
	calls, commits int
	rolled         bool
	query          string
	args           []any
}

func (tx *accountResolvedTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.calls++
	if strings.Contains(query, "photo_account_sizes") {
		tx.query, tx.args = query, append([]any(nil), args...)
		if tx.fail == "write" {
			return pgconn.CommandTag{}, errResolvedSizes
		}
		return pgconn.NewCommandTag("INSERT 0 " + strconv.FormatInt(tx.rows, 10)), nil
	}
	if strings.Contains(query, "outbox_events") {
		if len(args) != 2 || args[0] != app.SizesResolvedTopic {
			return pgconn.CommandTag{}, errors.New("unexpected sizes event")
		}
		if tx.fail == "event" {
			return pgconn.CommandTag{}, errResolvedSizes
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	return pgconn.CommandTag{}, errors.New("unexpected transaction query")
}

func (tx *accountResolvedTx) Commit(context.Context) error {
	tx.commits++
	if tx.fail == "commit" {
		return errResolvedSizes
	}
	return nil
}

func (tx *accountResolvedTx) Rollback(context.Context) error { tx.rolled = true; return nil }

type accountResolvedPool struct {
	tx  *accountResolvedTx
	err error
}

func (p accountResolvedPool) Begin(context.Context) (pgx.Tx, error) { return p.tx, p.err }

func TestSetAccountSizesStoresOnlyForCurrentOwnerAndPublishesAtomically(t *testing.T) {
	version := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	at := version.Add(time.Minute)
	tx := &accountResolvedTx{rows: 1}
	var published app.SizesResolved
	store := ResolvedSizes{
		Pool: accountResolvedPool{tx: tx},
		Publish: func(ctx context.Context, tx pgx.Tx, event app.SizesResolved) error {
			published = event
			return testPublishResolvedSizes(ctx, tx, event)
		},
	}
	if err := store.SetAccountSizes(t.Context(), 8, 13, 5, []byte(`[]`), domain.SizesConfirmed, version, "private", at); err != nil {
		t.Fatal(err)
	}
	if tx.calls != 2 || tx.commits != 1 || !tx.rolled || published.PhotoID != 8 || published.UserID != 5 || published.Status != string(domain.SizesConfirmed) {
		t.Fatalf("tx=%+v event=%+v", tx, published)
	}
	if !strings.Contains(tx.query, "a.id=$2 AND a.user_id=$3") || !strings.Contains(tx.query, "p.last_update=$4 AND p.visibility=$5") || tx.args[0] != int64(8) || tx.args[1] != int64(13) || tx.args[2] != int64(5) || tx.args[4] != "private" {
		t.Fatalf("account sizes query is not ownership and version scoped: %s args=%v", tx.query, tx.args)
	}
}

func TestSetAccountSizesRejectsInvalidAndFailedWrites(t *testing.T) {
	version := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	valid := func() ResolvedSizes {
		return ResolvedSizes{Pool: accountResolvedPool{tx: &accountResolvedTx{rows: 1}}, Publish: testPublishResolvedSizes}
	}
	if err := (ResolvedSizes{}).SetAccountSizes(t.Context(), 8, 13, 5, []byte(`[]`), domain.SizesConfirmed, version, "private", version); err == nil {
		t.Fatal("accepted missing dependencies")
	}
	if err := (ResolvedSizes{Pool: accountResolvedPool{tx: &accountResolvedTx{rows: 1}}}).SetAccountSizes(t.Context(), 8, 13, 5, []byte(`[]`), domain.SizesConfirmed, version, "private", version); err == nil {
		t.Fatal("accepted missing publisher")
	}
	for _, input := range []struct {
		photoID, accountID, userID int64
		sizes                      []byte
		status                     domain.SizesStatus
		version                    time.Time
		visibility                 string
		at                         time.Time
	}{
		{accountID: 13, userID: 5, sizes: []byte(`[]`), status: domain.SizesConfirmed, version: version, visibility: "private", at: version},
		{photoID: 8, userID: 5, sizes: []byte(`[]`), status: domain.SizesConfirmed, version: version, visibility: "private", at: version},
		{photoID: 8, accountID: 13, sizes: []byte(`[]`), status: domain.SizesConfirmed, version: version, visibility: "private", at: version},
		{photoID: 8, accountID: 13, userID: 5, sizes: []byte(`bad`), status: domain.SizesConfirmed, version: version, visibility: "private", at: version},
		{photoID: 8, accountID: 13, userID: 5, sizes: []byte(`[]`), status: domain.SizesProvisional, version: version, visibility: "private", at: version},
		{photoID: 8, accountID: 13, userID: 5, sizes: []byte(`[]`), status: domain.SizesConfirmed, visibility: "private", at: version},
		{photoID: 8, accountID: 13, userID: 5, sizes: []byte(`[]`), status: domain.SizesConfirmed, version: version, at: version},
		{photoID: 8, accountID: 13, userID: 5, sizes: []byte(`[]`), status: domain.SizesConfirmed, version: version, visibility: "private"},
	} {
		if err := valid().SetAccountSizes(t.Context(), input.photoID, input.accountID, input.userID, input.sizes, input.status, input.version, input.visibility, input.at); err == nil {
			t.Fatal("accepted invalid account sizes input")
		}
	}
	if err := valid().SetAccountSizes(nil, 8, 13, 5, []byte(`[]`), domain.SizesConfirmed, version, "private", version); err == nil {
		t.Fatal("accepted nil context")
	}
	for _, fail := range []string{"begin", "write", "rows", "event", "commit"} {
		t.Run(fail, func(t *testing.T) {
			tx := &accountResolvedTx{rows: 1, fail: fail}
			pool := accountResolvedPool{tx: tx}
			if fail == "begin" {
				pool.err = errResolvedSizes
			}
			if fail == "rows" {
				tx.rows = 0
			}
			err := (ResolvedSizes{Pool: pool, Publish: testPublishResolvedSizes}).SetAccountSizes(t.Context(), 8, 13, 5, []byte(`[]`), domain.SizesConfirmed, version, "private", version)
			if err == nil {
				t.Fatal("lost transaction failure")
			}
			if fail != "rows" && !errors.Is(err, errResolvedSizes) {
				t.Fatal(err)
			}
			if fail != "commit" && tx.commits != 0 {
				t.Fatal("failed write committed")
			}
		})
	}
}

func TestResolveUserSizesPublishesScopedContinuation(t *testing.T) {
	for _, status := range []domain.SizesStatus{domain.SizesConfirmed, domain.SizesUnavailable} {
		tx := &accountResolvedTx{rows: 1}
		var published app.SizesResolved
		store := ResolvedSizes{
			Pool: accountResolvedPool{tx: tx},
			Publish: func(ctx context.Context, tx pgx.Tx, event app.SizesResolved) error {
				published = event
				return testPublishResolvedSizes(ctx, tx, event)
			},
		}
		if err := store.ResolveUserSizes(t.Context(), 8, 5, status); err != nil {
			t.Fatal(err)
		}
		if published.PhotoID != 8 || published.UserID != 5 || published.Status != string(status) || tx.calls != 1 || tx.commits != 1 || !tx.rolled {
			t.Fatalf("tx=%+v event=%+v", tx, published)
		}
	}
	if err := (ResolvedSizes{}).ResolveUserSizes(t.Context(), 8, 5, domain.SizesConfirmed); err == nil {
		t.Fatal("accepted missing dependencies")
	}
	if err := (ResolvedSizes{Pool: accountResolvedPool{tx: &accountResolvedTx{rows: 1}}, Publish: testPublishResolvedSizes}).ResolveUserSizes(nil, 8, 5, domain.SizesConfirmed); err == nil {
		t.Fatal("accepted nil context")
	}
	for _, input := range []struct {
		photoID, userID int64
		status          domain.SizesStatus
	}{{userID: 5, status: domain.SizesConfirmed}, {photoID: 8, status: domain.SizesConfirmed}, {photoID: 8, userID: 5, status: domain.SizesProvisional}} {
		if err := (ResolvedSizes{Pool: accountResolvedPool{tx: &accountResolvedTx{rows: 1}}, Publish: testPublishResolvedSizes}).ResolveUserSizes(t.Context(), input.photoID, input.userID, input.status); err == nil {
			t.Fatal("accepted invalid user continuation")
		}
	}
	for _, fail := range []string{"begin", "event", "commit"} {
		t.Run(fail, func(t *testing.T) {
			tx := &accountResolvedTx{rows: 1, fail: fail}
			pool := accountResolvedPool{tx: tx}
			if fail == "begin" {
				pool.err = errResolvedSizes
			}
			err := (ResolvedSizes{Pool: pool, Publish: testPublishResolvedSizes}).ResolveUserSizes(t.Context(), 8, 5, domain.SizesConfirmed)
			if err == nil || !errors.Is(err, errResolvedSizes) {
				t.Fatalf("lost transaction failure: %v", err)
			}
			if fail != "commit" && tx.commits != 0 {
				t.Fatal("failed event committed")
			}
		})
	}
}
