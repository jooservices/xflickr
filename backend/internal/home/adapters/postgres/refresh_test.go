package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/home/app"
)

type refreshDBStub struct {
	row        pgx.Row
	sharingRow pgx.Row
	tx         pgx.Tx
	beginErr   error
}

func (d *refreshDBStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if strings.Contains(query, "user_home_sharing_syncs") {
		if d.sharingRow != nil {
			return d.sharingRow
		}
		return refreshRowStub{values: []any{false}}
	}
	return d.row
}

func (d *refreshDBStub) Begin(context.Context) (pgx.Tx, error) {
	return d.tx, d.beginErr
}

type refreshTxStub struct {
	pgx.Tx
	lockRow    pgx.Row
	rows       pgx.Rows
	queryErr   error
	execQuery  string
	execErr    error
	sharingRow pgx.Row
	execCalls  int
	commitErr  error
}

func (tx *refreshTxStub) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return tx.rows, tx.queryErr
}

func (tx *refreshTxStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if strings.Contains(query, "SELECT EXISTS (SELECT 1 FROM user_home_sharing_syncs)") {
		if tx.sharingRow != nil {
			return tx.sharingRow
		}
		return refreshRowStub{values: []any{false}}
	}
	if strings.Contains(query, "FROM user_home_sharing_syncs WHERE user_id") {
		return refreshRowStub{err: pgx.ErrNoRows}
	}
	return tx.lockRow
}

func (tx *refreshTxStub) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	if tx.execErr != nil && strings.Contains(query, tx.execQuery) {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *refreshTxStub) Commit(context.Context) error   { return tx.commitErr }
func (tx *refreshTxStub) Rollback(context.Context) error { return nil }

type refreshRowStub struct {
	values []any
	err    error
}

func (row refreshRowStub) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	for i, value := range row.values {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *int64:
			*target = value.(int64)
		case *bool:
			*target = value.(bool)
		}
	}
	return nil
}

type refreshRowsStub struct {
	pgx.Rows
	values       [][]any
	index        int
	scanErr      error
	iterationErr error
}

func (rows *refreshRowsStub) Next() bool {
	if rows.index >= len(rows.values) {
		return false
	}
	rows.index++
	return true
}

func (rows *refreshRowsStub) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	for i, value := range rows.values[rows.index-1] {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *bool:
			*target = value.(bool)
		case *int64:
			*target = value.(int64)
		}
	}
	return nil
}

func (*refreshRowsStub) Close()          {}
func (rows *refreshRowsStub) Err() error { return rows.iterationErr }

func TestRefreshBatchRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		repo *Repository
		ctx  context.Context
		user int64
		size int
	}{
		{name: "nil repository", ctx: t.Context(), user: 1, size: 1},
		{name: "nil context", repo: &Repository{db: &queryerStub{}}, user: 1, size: 1},
		{name: "invalid user", repo: &Repository{db: &queryerStub{}}, ctx: t.Context(), size: 1},
		{name: "empty batch", repo: &Repository{db: &queryerStub{}}, ctx: t.Context(), user: 1},
		{name: "oversized batch", repo: &Repository{db: &queryerStub{}}, ctx: t.Context(), user: 1, size: 251},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.repo.RefreshBatch(test.ctx, test.user, test.size); err == nil {
				t.Fatal("RefreshBatch() succeeded")
			}
		})
	}
}

func TestRefreshBatchReportsDatabaseFailures(t *testing.T) {
	want := errors.New("database unavailable")
	for _, test := range []struct {
		name string
		db   *refreshDBStub
		tx   *refreshTxStub
	}{
		{name: "begin", db: &refreshDBStub{beginErr: want}},
		{name: "sharing sync check", tx: &refreshTxStub{sharingRow: refreshRowStub{err: want}}},
		{name: "start", tx: &refreshTxStub{execQuery: "INSERT INTO user_home_stats_refreshes", execErr: want}},
		{name: "lock", tx: &refreshTxStub{lockRow: refreshRowStub{err: want}}},
		{name: "page query", tx: &refreshTxStub{lockRow: refreshRowStub{}, queryErr: want}},
		{name: "page scan", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{values: [][]any{{"1", "owner", true, true, true}}, scanErr: want}}},
		{name: "page iteration", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{iterationErr: want}}},
		{name: "advance", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{values: [][]any{{"1", "owner", true, true, true}}}, execQuery: "UPDATE user_home_stats_refreshes", execErr: want}},
		{name: "contact counts", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{values: [][]any{{"1", "owner", true, true, true}}}, execQuery: "INSERT INTO user_home_contact_stats_refreshes", execErr: want}},
		{name: "publish totals", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{values: [][]any{{"1", "owner", true, true, true}}}, execQuery: "INSERT INTO user_home_stats\n", execErr: want}},
		{name: "replace contact counts", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{}, execQuery: "DELETE FROM user_home_contact_stats WHERE", execErr: want}},
		{name: "copy contact counts", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{}, execQuery: "INSERT INTO user_home_contact_stats\n", execErr: want}},
		{name: "clear staged contacts", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{}, execQuery: "DELETE FROM user_home_contact_stats_refreshes", execErr: want}},
		{name: "clear refresh", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{}, execQuery: "DELETE FROM user_home_stats_refreshes WHERE", execErr: want}},
		{name: "commit", tx: &refreshTxStub{lockRow: refreshRowStub{}, rows: &refreshRowsStub{}, commitErr: want}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := test.tx
			if tx == nil {
				tx = &refreshTxStub{}
			}
			if tx.lockRow == nil {
				tx.lockRow = refreshRowStub{values: []any{"", int64(0), int64(0), int64(0), int64(0), int64(0)}}
			}
			if tx.rows == nil {
				tx.rows = &refreshRowsStub{}
			}
			db := test.db
			if db == nil {
				db = &refreshDBStub{row: refreshRowStub{values: []any{false}}, tx: tx}
			} else {
				db.tx = tx
				db.row = refreshRowStub{values: []any{false}}
			}
			repository := &Repository{db: db}
			_, err := repository.RefreshBatch(t.Context(), 1, 2)
			if !errors.Is(err, want) {
				t.Fatalf("RefreshBatch() error = %v, want wrapped failure", err)
			}
		})
	}
}

func TestRefreshBatchRequiresRefreshDatabase(t *testing.T) {
	if _, err := (&Repository{db: &queryerStub{}}).RefreshBatch(t.Context(), 1, 1); !errors.Is(err, errRefreshDatabaseRequired) {
		t.Fatalf("RefreshBatch() error = %v, want missing database support", err)
	}
}

func TestRefreshBatchWaitsForSharingCandidateSyncs(t *testing.T) {
	pending := refreshRowStub{values: []any{true}}
	tx := &refreshTxStub{sharingRow: pending, rows: &refreshRowsStub{}}
	database := &refreshDBStub{row: refreshRowStub{values: []any{false}}, sharingRow: pending, tx: tx}
	repository := &Repository{db: database}
	done, err := repository.RefreshBatch(t.Context(), 9, 2)
	if err != nil || done || tx.execCalls != 0 {
		t.Fatalf("refresh during a sharing sync = done %t, err %v, writes %d", done, err, tx.execCalls)
	}
}

func TestConfiguredRefreshBatchSizeReadsAdminSetting(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   int64
		rowErr  error
		want    int
		wantErr bool
	}{
		{name: "configured value", value: 750, want: 750},
		{name: "below minimum", value: 249, wantErr: true},
		{name: "above maximum", value: 1001, wantErr: true},
		{name: "database error", rowErr: errors.New("database unavailable"), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := &queryerStub{row: stubRow{values: []any{test.value}, err: test.rowErr}}
			got, err := configuredRefreshBatchSize(t.Context(), database, app.RefreshBatchSize)
			if (err != nil) != test.wantErr {
				t.Fatalf("configuredRefreshBatchSize() error = %v, wantErr %t", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("configuredRefreshBatchSize() = %d, want %d", got, test.want)
			}
			if !strings.Contains(database.query, "home.stats_refresh_batch_size") {
				t.Fatalf("batch setting query = %q", database.query)
			}
		})
	}
}

func TestRefreshPagePaginatesIndexedPhotoSources(t *testing.T) {
	for _, fragment := range []string{
		"FROM user_home_photo_candidates candidate",
		"FROM user_home_shared_photo_candidates shared_candidate",
		"candidate.photo_id > COALESCE(NULLIF($2, '')::bigint, 0)",
		"shared_candidate.photo_id > COALESCE(NULLIF($2, '')::bigint, 0)",
		"ORDER BY candidate.photo_id",
		"ORDER BY shared_candidate.photo_id",
		"LIMIT $3",
	} {
		if !strings.Contains(refreshPhotoPageSQL, fragment) {
			t.Errorf("refresh page SQL is missing %q: %s", fragment, refreshPhotoPageSQL)
		}
	}
	if strings.Contains(refreshPhotoPageSQL, "FROM observations") {
		t.Fatal("refresh pages must use the bounded shared candidate index")
	}
}
