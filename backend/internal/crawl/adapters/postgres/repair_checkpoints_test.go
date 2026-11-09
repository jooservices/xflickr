package postgres

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type repairDB struct {
	beginErr    error
	queryErr    string
	commitErrAt int
	results     []repairBatch
	txs         []*repairTx
}

func (d *repairDB) Begin(context.Context) (CheckpointRepairTx, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	index := len(d.txs)
	var result repairBatch
	if index < len(d.results) {
		result = d.results[index]
	}
	tx := &repairTx{result: result, queryErr: d.queryErr}
	if d.commitErrAt == index+1 {
		tx.commitErr = errors.New("commit failed")
	}
	d.txs = append(d.txs, tx)
	return tx, nil
}

type repairTx struct {
	result     repairBatch
	queryErr   string
	commitErr  error
	querySQL   string
	queryArgs  []any
	rollbacked bool
	committed  bool
}

func (t *repairTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	t.querySQL = query
	t.queryArgs = append([]any(nil), args...)
	if t.queryErr != "" && strings.Contains(query, t.queryErr) {
		return repairRow{err: errors.New("query failed")}
	}
	return repairRow{batch: t.result}
}

func (t *repairTx) Commit(context.Context) error {
	t.committed = true
	return t.commitErr
}

func (t *repairTx) Rollback(context.Context) error {
	t.rollbacked = true
	return nil
}

type repairRow struct {
	batch repairBatch
	err   error
}

func (r repairRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 5 {
		return errors.New("unexpected repair scan arity")
	}
	*dest[0].(*int64) = r.batch.Selected
	*dest[1].(*int64) = r.batch.Updated
	*dest[2].(*int64) = r.batch.Unrepairable
	*dest[3].(*int64) = r.batch.Last.UserID
	*dest[4].(*string) = r.batch.Last.PersonNSID
	return nil
}

func TestRepairZeroCheckpointsRejectsBadOptions(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	options := CheckpointRepairOptions{BatchSize: 10, Now: func() time.Time { return now }}
	if _, err := RepairZeroCheckpoints(t.Context(), &repairDB{}, CheckpointRepairOptions{}); err == nil {
		t.Fatal("empty options accepted")
	}
	if _, err := RepairZeroCheckpoints(nil, &repairDB{}, options); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), nil, options); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &repairDB{}, CheckpointRepairOptions{BatchSize: 501}); err == nil {
		t.Fatal("batch size above 500 accepted")
	}
	report, err := RepairZeroCheckpoints(t.Context(), &repairDB{}, options)
	if err != nil || !report.RunAt.Equal(now) {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestRepairZeroCheckpointsRunsEmptyBatches(t *testing.T) {
	db := &repairDB{}
	report, err := RepairZeroCheckpoints(t.Context(), db, CheckpointRepairOptions{
		BatchSize: 10,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.PhotosUpdated != 0 || report.FavoritesUpdated != 0 || report.Batches != 2 {
		t.Fatalf("report=%+v", report)
	}
	if len(db.txs) != 2 || !db.txs[0].committed || !db.txs[1].committed {
		t.Fatalf("txs=%+v", db.txs)
	}
}

func TestRepairZeroCheckpointsUpdatesUntilEmpty(t *testing.T) {
	db := &repairDB{results: []repairBatch{
		{Selected: 3, Updated: 3, Last: repairCursor{UserID: 1, PersonNSID: "c"}},
		{},
		{},
	}}
	report, err := RepairZeroCheckpoints(t.Context(), db, CheckpointRepairOptions{BatchSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if report.PhotosUpdated != 3 || report.FavoritesUpdated != 0 || report.Batches != 3 {
		t.Fatalf("report=%+v", report)
	}
}

func TestRepairZeroCheckpointsContinuesPastUnrepairableBatch(t *testing.T) {
	db := &repairDB{results: []repairBatch{
		{Selected: 2, Unrepairable: 2, Last: repairCursor{UserID: 7, PersonNSID: "b"}},
		{Selected: 2, Updated: 1, Unrepairable: 1, Last: repairCursor{UserID: 7, PersonNSID: "d"}},
		{},
		{},
	}}
	report, err := RepairZeroCheckpoints(t.Context(), db, CheckpointRepairOptions{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if report.PhotosUpdated != 1 || report.PhotosUnrepairable != 3 || report.Batches != 4 {
		t.Fatalf("report=%+v", report)
	}
	if len(db.txs) != 4 || db.txs[1].queryArgs[1] != int64(7) || db.txs[1].queryArgs[2] != "b" {
		t.Fatalf("second batch did not resume after first batch's last key: %+v", db.txs)
	}
	for _, tx := range db.txs[:3] {
		if !strings.Contains(tx.querySQL, "FROM work_items") || !strings.Contains(tx.querySQL, "scan_completed_at") ||
			!strings.Contains(tx.querySQL, "w.user_id = b.user_id") || !strings.Contains(tx.querySQL, "w.subject_key = b.person_nsid") {
			t.Fatalf("repair query lacks completed observer-scoped scan lookup: %s", tx.querySQL)
		}
		if strings.Contains(tx.querySQL, "FROM favorites") || strings.Contains(tx.querySQL, "FROM photos") {
			t.Fatalf("repair query scans shared catalog data: %s", tx.querySQL)
		}
	}
}

func TestRepairQueryUsesExplicitCompletionAndSafeLegacyFloor(t *testing.T) {
	db := &repairDB{results: []repairBatch{{Selected: 1, Unrepairable: 1, Last: repairCursor{UserID: 1, PersonNSID: "person"}}}}
	_, err := RepairZeroCheckpoints(t.Context(), db, CheckpointRepairOptions{BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	query := db.txs[0].querySQL
	if !strings.Contains(query, "scan_completed_at") || !strings.Contains(query, "THEN 1") {
		t.Fatalf("repair query must use a final-page marker and an epoch catch-up floor: %s", query)
	}
	if strings.Contains(query, "SKIP LOCKED") || !strings.Contains(query, "FOR UPDATE") {
		t.Fatalf("repair query can silently pass locked interests: %s", query)
	}
}

func TestRepairZeroCheckpointsRejectsInvalidBatchProgress(t *testing.T) {
	tests := []struct {
		name   string
		result repairBatch
	}{
		{name: "missing keyset key", result: repairBatch{Selected: 1, Unrepairable: 1}},
		{name: "inconsistent counts", result: repairBatch{Selected: 1, Updated: 1, Unrepairable: 1, Last: repairCursor{UserID: 1, PersonNSID: "a"}}},
		{name: "unclassified row", result: repairBatch{Selected: 1, Last: repairCursor{UserID: 1, PersonNSID: "a"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := &repairDB{results: []repairBatch{tc.result}}
			if _, err := RepairZeroCheckpoints(t.Context(), db, CheckpointRepairOptions{BatchSize: 1}); err == nil {
				t.Fatal("invalid progress accepted")
			}
		})
	}
}

func TestRepairBatchErrorsSurface(t *testing.T) {
	options := CheckpointRepairOptions{BatchSize: 1}
	if _, err := RepairZeroCheckpoints(t.Context(), &repairDB{beginErr: errors.New("begin")}, options); err == nil {
		t.Fatal("begin error ignored")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &repairDB{queryErr: "last_seen_at"}, options); err == nil {
		t.Fatal("photo query error ignored")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &repairDB{queryErr: "last_fave_at"}, options); err == nil {
		t.Fatal("favorite query error ignored")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &repairDB{commitErrAt: 1}, options); err == nil {
		t.Fatal("photo commit error ignored")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &repairDB{commitErrAt: 2}, options); err == nil {
		t.Fatal("favorite commit error ignored")
	}
}

func TestRepairZeroCheckpointsPoolRequiresPool(t *testing.T) {
	if _, err := RepairZeroCheckpointsPool(t.Context(), nil, CheckpointRepairOptions{BatchSize: 1}); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestRepairUnsupportedKindRejected(t *testing.T) {
	if _, err := repairZeroCheckpointsBatch(t.Context(), &repairDB{}, 1, repairCursor{}, "albums"); err == nil {
		t.Fatal("unsupported repair kind accepted")
	}
}
