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
	for _, target := range dest {
		*target.(*int64) = r.count
	}
	return nil
}

type progressConn struct {
	row        progressRow
	queryCalls int
	execCalls  int
	query      string
	queryText  string
	args       []any
	queryArgs  []any
	execErr    error
}

func (c *progressConn) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	c.execCalls++
	c.query, c.args = query, args
	return pgconn.CommandTag{}, c.execErr
}
func (c *progressConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *progressConn) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	c.queryCalls++
	c.queryText, c.queryArgs = query, args
	return c.row
}
func (*progressConn) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

func TestContactProgressKeepsPageCountsStableAndDeduplicatesIncrementalPages(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		item         domain.Item
		result       domain.Result
		wantQueries  int
		wantExecs    int
		wantCount    int64
		wantReset    bool
		wantCountSQL string
	}{
		{
			name:        "full photo page resets summary",
			item:        domain.Item{Kind: "photos", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1, Params: map[string]string{"per_page": "2"}}},
			result:      domain.Result{Photos: []catalogdomain.Photo{{ID: 11}, {ID: 12}}},
			wantQueries: 2, wantExecs: 2, wantReset: true, wantCount: 2,
		},
		{
			name:         "incremental photo page adds only unseen IDs",
			item:         domain.Item{Kind: "photos", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1, Params: map[string]string{"min_upload_date": "42"}}},
			result:       domain.Result{Photos: []catalogdomain.Photo{{ID: 11}, {ID: 12}, {ID: 12}}},
			wantQueries:  2,
			wantExecs:    2,
			wantCount:    2,
			wantCountSQL: "observations",
		},
		{
			name:        "later full photo page records an absolute offset",
			item:        domain.Item{Kind: "photos", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 3, Params: map[string]string{"per_page": "2"}}},
			result:      domain.Result{Photos: []catalogdomain.Photo{{ID: 11}, {ID: 12}}},
			wantQueries: 2, wantExecs: 2, wantCount: 6,
		},
		{
			name:      "favorites full scan resets on first page",
			item:      domain.Item{Kind: "favorites", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1}},
			result:    domain.Result{Favorites: []catalogdomain.Favorite{{PhotoID: 11}, {PhotoID: 12}}},
			wantExecs: 1,
			wantCount: 2,
			wantReset: true,
		},
		{
			name:         "incremental favorites add only missing relationships",
			item:         domain.Item{Kind: "favorites", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1, Params: map[string]string{"min_fave_date": "42"}}},
			result:       domain.Result{Favorites: []catalogdomain.Favorite{{PhotoID: 11}, {PhotoID: 12}}},
			wantQueries:  1,
			wantExecs:    1,
			wantCount:    2,
			wantCountSQL: "favorites",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &progressConn{row: progressRow{count: test.wantCount}}
			progress := contactProgressTx{tx: conn}
			if err := progress.BeginPage(t.Context(), test.item, test.result); err != nil {
				t.Fatal(err)
			}
			progressQuery := conn.query
			progressArgs := conn.args
			if len(progressArgs) < 4 || progressArgs[3] != test.wantCount {
				t.Fatalf("summary query=%q args=%v, count=%d", progressQuery, progressArgs, test.wantCount)
			}
			if test.wantReset && !strings.Contains(progressQuery, "photos_crawled=CASE") && !strings.Contains(progressQuery, "favorites_count=CASE") {
				t.Fatalf("full scan did not assign an absolute count: %q", progressQuery)
			}
			if err := progress.Record(t.Context(), test.item, test.result); err != nil {
				t.Fatal(err)
			}
			if conn.execCalls != test.wantExecs || conn.queryCalls != test.wantQueries {
				t.Fatalf("calls: query=%d exec=%d", conn.queryCalls, conn.execCalls)
			}
			if test.wantCountSQL != "" && !strings.Contains(conn.queryText, test.wantCountSQL) {
				t.Fatalf("dedupe query=%q", conn.queryText)
			}
		})
	}
}

func TestContactProgressSkipsUnscopedAndUnrelatedPagesAndWrapsFailures(t *testing.T) {
	t.Parallel()
	for _, item := range []domain.Item{{Kind: "photos", Subject: "one@N00"}, {Kind: "other", UserID: 7, Subject: "one@N00"}} {
		conn := &progressConn{}
		progress := contactProgressTx{tx: conn}
		if err := progress.BeginPage(t.Context(), item, domain.Result{}); err != nil || conn.execCalls != 0 {
			t.Fatalf("item %+v calls=%d err=%v", item, conn.execCalls, err)
		}
	}
	queryFailure := errors.New("query failed")
	conn := &progressConn{row: progressRow{err: queryFailure}}
	item := domain.Item{Kind: "photos", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1, Params: map[string]string{"min_upload_date": "42"}}}
	err := (contactProgressTx{tx: conn}).BeginPage(t.Context(), item, domain.Result{Photos: []catalogdomain.Photo{{ID: 2}}})
	if !errors.Is(err, queryFailure) || !strings.Contains(err.Error(), "count new contact photos") {
		t.Fatalf("photo dedupe query error=%v", err)
	}
	execFailure := errors.New("write failed")
	conn = &progressConn{row: progressRow{count: 1}, execErr: execFailure}
	err = (contactProgressTx{tx: conn}).BeginPage(t.Context(), domain.Item{Kind: "favorites", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1}}, domain.Result{})
	if !errors.Is(err, execFailure) || !strings.Contains(err.Error(), "update contact favorites progress") {
		t.Fatalf("favorite summary write error=%v", err)
	}
	conn = &progressConn{row: progressRow{err: queryFailure}}
	err = (contactProgressTx{tx: conn}).Record(t.Context(), domain.Item{Kind: "photos", UserID: 7, Subject: "one@N00", Cursor: domain.Cursor{Page: 1}}, domain.Result{Photos: []catalogdomain.Photo{{ID: 2}}})
	if !errors.Is(err, queryFailure) || !strings.Contains(err.Error(), "count confirmed contact photo sizes") {
		t.Fatalf("photo size query error=%v", err)
	}
}
