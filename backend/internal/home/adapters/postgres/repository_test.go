package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jooservices/xflickr/internal/home/app"
)

type fakeRow struct {
	direct, discovered int64
	err                error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int64)) = r.direct
	*(dest[1].(*int64)) = r.discovered
	return nil
}

type fakeQueryer struct {
	query string
	args  []any
	row   pgx.Row
}

func (q *fakeQueryer) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	q.query, q.args = query, args
	return q.row
}

func TestRepositoryContactDepthsUsesGroupedUserScope(t *testing.T) {
	queryer := &fakeQueryer{row: fakeRow{direct: 2, discovered: 4}}
	repository := &Repository{pool: queryer}
	got, err := repository.ContactDepths(t.Context(), 17)
	if err != nil {
		t.Fatal(err)
	}
	if got != (app.ContactDepthCounts{Direct: 2, Discovered: 4}) {
		t.Fatalf("ContactDepths() = %+v", got)
	}
	if len(queryer.args) != 1 || queryer.args[0] != int64(17) {
		t.Fatalf("query args = %v, want user id 17", queryer.args)
	}
	for _, fragment := range []string{"FROM crawl_interests", "WHERE user_id = $1", "GROUP BY depth", "depth = 1", "depth > 1"} {
		if !strings.Contains(queryer.query, fragment) {
			t.Errorf("query %q does not contain %q", queryer.query, fragment)
		}
	}
}

func TestRepositoryContactDepthsWrapsScanError(t *testing.T) {
	repository := &Repository{pool: &fakeQueryer{row: fakeRow{err: errors.New("scan failed")}}}
	if _, err := repository.ContactDepths(t.Context(), 17); err == nil || !strings.Contains(err.Error(), "home: read contact depths") {
		t.Fatalf("ContactDepths() error = %v", err)
	}
}

func TestRepositoryRejectsInvalidInputs(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New() accepted a nil database")
	}
	valid := &Repository{pool: &fakeQueryer{row: fakeRow{}}}
	for _, tc := range []struct {
		name       string
		repository *Repository
		ctx        context.Context
		userID     int64
	}{
		{name: "nil repository", ctx: t.Context(), userID: 1},
		{name: "nil context", repository: valid, userID: 1},
		{name: "invalid user", repository: valid, ctx: t.Context()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.repository.ContactDepths(tc.ctx, tc.userID); err == nil {
				t.Fatal("ContactDepths() accepted invalid input")
			}
		})
	}
}
