package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type sharingSyncTxStub struct {
	pgx.Tx
	row         pgx.Row
	rows        pgx.Rows
	queryErr    error
	execErr     error
	failQuery   string
	execQueries []string
}

func (tx *sharingSyncTxStub) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return tx.rows, tx.queryErr
}

func (tx *sharingSyncTxStub) QueryRow(context.Context, string, ...any) pgx.Row {
	return tx.row
}

func (tx *sharingSyncTxStub) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	tx.execQueries = append(tx.execQueries, query)
	if tx.execErr != nil && strings.Contains(query, tx.failQuery) {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *sharingSyncTxStub) Commit(context.Context) error   { return nil }
func (tx *sharingSyncTxStub) Rollback(context.Context) error { return nil }

func TestSyncSharingCandidatesBatchBoundsAndCompletesPages(t *testing.T) {
	for _, test := range []struct {
		name        string
		enabled     bool
		cursor      int64
		photoIDs    []int64
		batchSize   int
		wantPending bool
		wantQuery   string
	}{
		{name: "enabled final page", enabled: true, photoIDs: []int64{11}, batchSize: 2, wantQuery: "INSERT INTO user_home_shared_photo_candidates"},
		{name: "disabled full page", enabled: false, cursor: 10, photoIDs: []int64{11, 12}, batchSize: 2, wantPending: true, wantQuery: "DELETE FROM user_home_shared_photo_candidates"},
		{name: "empty final page", enabled: false, cursor: 12, batchSize: 2, wantQuery: "DELETE FROM user_home_sharing_syncs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &sharingSyncTxStub{
				row:  refreshRowStub{values: []any{test.enabled, test.cursor}},
				rows: &refreshRowsStub{values: photoRows(test.photoIDs)},
			}
			pending, err := syncSharingCandidatesBatch(t.Context(), tx, 9, test.batchSize)
			if err != nil || pending != test.wantPending {
				t.Fatalf("syncSharingCandidatesBatch() = %t, %v; want %t, nil", pending, err, test.wantPending)
			}
			if !hasQuery(tx.execQueries, test.wantQuery) {
				t.Fatalf("sync queries %q do not include %q", tx.execQueries, test.wantQuery)
			}
		})
	}
}

func TestSyncSharingCandidatesBatchSkipsWhenNoSyncIsPending(t *testing.T) {
	tx := &sharingSyncTxStub{row: refreshRowStub{err: pgx.ErrNoRows}}
	pending, err := syncSharingCandidatesBatch(t.Context(), tx, 9, 2)
	if err != nil || pending || len(tx.execQueries) != 0 {
		t.Fatalf("sync without pending work = %t, %v, queries=%q", pending, err, tx.execQueries)
	}
}

func TestSyncSharingCandidatesBatchReportsFailures(t *testing.T) {
	want := errors.New("database unavailable")
	for _, test := range []struct {
		name      string
		row       pgx.Row
		rows      pgx.Rows
		queryErr  error
		failQuery string
		batchSize int
	}{
		{name: "lock", row: refreshRowStub{err: want}},
		{name: "page query", row: refreshRowStub{values: []any{true, int64(0)}}, queryErr: want},
		{name: "page scan", row: refreshRowStub{values: []any{true, int64(0)}}, rows: &refreshRowsStub{values: photoRows([]int64{1}), scanErr: want}},
		{name: "page iteration", row: refreshRowStub{values: []any{true, int64(0)}}, rows: &refreshRowsStub{iterationErr: want}},
		{name: "add candidates", row: refreshRowStub{values: []any{true, int64(0)}}, rows: &refreshRowsStub{values: photoRows([]int64{1})}, failQuery: "INSERT INTO user_home_shared_photo_candidates"},
		{name: "remove candidates", row: refreshRowStub{values: []any{false, int64(0)}}, rows: &refreshRowsStub{values: photoRows([]int64{1})}, failQuery: "DELETE FROM user_home_shared_photo_candidates"},
		{name: "advance cursor", row: refreshRowStub{values: []any{true, int64(0)}}, rows: &refreshRowsStub{values: photoRows([]int64{1})}, batchSize: 1, failQuery: "UPDATE user_home_sharing_syncs"},
		{name: "complete sync", row: refreshRowStub{values: []any{false, int64(0)}}, rows: &refreshRowsStub{}, failQuery: "DELETE FROM user_home_sharing_syncs"},
		{name: "publish candidates", row: refreshRowStub{values: []any{false, int64(0)}}, rows: &refreshRowsStub{}, failQuery: "UPDATE user_home_shared_version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &sharingSyncTxStub{row: test.row, rows: test.rows, queryErr: test.queryErr, execErr: want, failQuery: test.failQuery}
			batchSize := test.batchSize
			if batchSize == 0 {
				batchSize = 2
			}
			_, err := syncSharingCandidatesBatch(t.Context(), tx, 9, batchSize)
			if !errors.Is(err, want) {
				t.Fatalf("syncSharingCandidatesBatch() error = %v, want database failure", err)
			}
		})
	}
}

func photoRows(ids []int64) [][]any {
	rows := make([][]any, len(ids))
	for i, id := range ids {
		rows[i] = []any{id}
	}
	return rows
}

func hasQuery(queries []string, fragment string) bool {
	for _, query := range queries {
		if strings.Contains(query, fragment) {
			return true
		}
	}
	return false
}
