package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	catalogdomain "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
)

type progressRow struct {
	count int64
	err   error
}

func (r progressRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*int64) = r.count
	return nil
}

type progressConn struct {
	row        progressRow
	queryCalls int
	execCalls  int
	query      string
	args       []any
	execErr    error
}

func (c *progressConn) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	c.execCalls++
	c.query, c.args = query, args
	return pgconn.CommandTag{}, c.execErr
}
func (c *progressConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *progressConn) QueryRow(context.Context, string, ...any) pgx.Row {
	c.queryCalls++
	return c.row
}
func (*progressConn) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

func TestContactProgressRecordsUserScopedPhotoAndFavoritePages(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		item        domain.Item
		result      domain.Result
		wantQueries int
		wantReset   bool
		wantCount   int
	}{
		{
			name:        "full photo page resets summary",
			item:        domain.Item{Kind: "photos", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1}},
			result:      domain.Result{Photos: []catalogdomain.Photo{{ID: 11}, {ID: 12}}},
			wantQueries: 1, wantReset: true, wantCount: 2,
		},
		{
			name:      "incremental photo page preserves summary",
			item:      domain.Item{Kind: "photos", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1, Params: map[string]string{"min_upload_date": "42"}}},
			wantReset: false,
		},
		{
			name:      "favorites page resets full scan",
			item:      domain.Item{Kind: "favorites", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1}},
			result:    domain.Result{Favorites: []catalogdomain.Favorite{{PhotoID: 11}}},
			wantReset: true, wantCount: 1,
		},
		{
			name:      "favorites delta preserves summary",
			item:      domain.Item{Kind: "favorites", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1, Params: map[string]string{"min_fave_date": "42"}}},
			wantReset: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &progressConn{row: progressRow{count: 1}}
			err := (contactProgressTx{tx: conn}).Record(t.Context(), test.item, test.result)
			if err != nil || conn.execCalls != 1 || conn.queryCalls != test.wantQueries {
				t.Fatalf("calls: query=%d exec=%d err=%v", conn.queryCalls, conn.execCalls, err)
			}
			if !strings.Contains(conn.query, "CASE WHEN $3 THEN") || conn.args[2] != test.wantReset || conn.args[3] != test.wantCount {
				t.Fatalf("summary query=%q args=%v", conn.query, conn.args)
			}
		})
	}
}

func TestContactProgressSkipsUnscopedAndUnrelatedPagesAndWrapsFailures(t *testing.T) {
	t.Parallel()
	for _, item := range []domain.Item{{Kind: "photos", Subject: "one@N00"}, {Kind: "other", UserID: 7, Subject: "one@N00"}} {
		conn := &progressConn{}
		if err := (contactProgressTx{tx: conn}).Record(t.Context(), item, domain.Result{}); err != nil || conn.execCalls != 0 {
			t.Fatalf("item %+v calls=%d err=%v", item, conn.execCalls, err)
		}
	}
	queryFailure := errors.New("query failed")
	conn := &progressConn{row: progressRow{err: queryFailure}}
	err := (contactProgressTx{tx: conn}).Record(t.Context(), domain.Item{Kind: "photos", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1}}, domain.Result{Photos: []catalogdomain.Photo{{ID: 2}}})
	if !errors.Is(err, queryFailure) || !strings.Contains(err.Error(), "count confirmed contact photo sizes") {
		t.Fatalf("photo summary query error=%v", err)
	}
	execFailure := errors.New("write failed")
	conn = &progressConn{row: progressRow{count: 1}, execErr: execFailure}
	err = (contactProgressTx{tx: conn}).Record(t.Context(), domain.Item{Kind: "favorites", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1}}, domain.Result{})
	if !errors.Is(err, execFailure) || !strings.Contains(err.Error(), "update contact favorites progress") {
		t.Fatalf("favorite summary write error=%v", err)
	}
}
