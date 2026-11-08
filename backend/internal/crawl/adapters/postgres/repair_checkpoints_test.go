package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type repairDB struct {
	beginErr error
	txs      []*repairTx
}

func (d *repairDB) Begin(context.Context) (CheckpointRepairTx, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	tx := &repairTx{}
	d.txs = append(d.txs, tx)
	return tx, nil
}

type repairTx struct {
	execs      int
	affected   int64
	failExec   string
	commitErr  error
	rollbacked bool
	committed  bool
}

func (t *repairTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	t.execs++
	if t.failExec != "" && strings.Contains(sql, t.failExec) {
		return pgconn.CommandTag{}, errors.New("exec failed")
	}
	return pgconn.NewCommandTag("UPDATE " + string(rune('0'+t.affected))), nil
}

func (t *repairTx) Commit(context.Context) error {
	t.committed = true
	return t.commitErr
}

func (t *repairTx) Rollback(context.Context) error {
	t.rollbacked = true
	return nil
}

func TestRepairZeroCheckpointsRejectsBadOptions(t *testing.T) {
	_, err := RepairZeroCheckpoints(t.Context(), &repairDB{}, CheckpointRepairOptions{})
	if err == nil {
		t.Fatal("empty options accepted")
	}
	_, err = RepairZeroCheckpoints(nil, &repairDB{}, CheckpointRepairOptions{BatchSize: 10})
	if err == nil {
		t.Fatal("nil context accepted")
	}
	_, err = RepairZeroCheckpoints(t.Context(), nil, CheckpointRepairOptions{BatchSize: 10})
	if err == nil {
		t.Fatal("nil database accepted")
	}
	_, err = RepairZeroCheckpoints(t.Context(), &repairDB{}, CheckpointRepairOptions{BatchSize: 501})
	if err == nil {
		t.Fatal("batch size above 500 accepted")
	}
	if CheckpointRepairMargin != time.Hour {
		t.Fatalf("safety margin = %s, want 1h", CheckpointRepairMargin)
	}
}

func TestRepairZeroCheckpointsRunsEmptyBatches(t *testing.T) {
	db := &repairDB{}
	report, err := RepairZeroCheckpoints(t.Context(), db, CheckpointRepairOptions{
		BatchSize: 10,
		Now:       func() time.Time { return time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC) },
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
	seq := &sequentialRepairDB{affected: []int64{3, 0, 0}}
	report, err := RepairZeroCheckpoints(t.Context(), seq, CheckpointRepairOptions{BatchSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if report.PhotosUpdated != 3 || report.FavoritesUpdated != 0 || report.Batches != 3 {
		t.Fatalf("report=%+v", report)
	}
}

type sequentialRepairDB struct {
	affected []int64
	i        int
}

func (d *sequentialRepairDB) Begin(context.Context) (CheckpointRepairTx, error) {
	n := int64(0)
	if d.i < len(d.affected) {
		n = d.affected[d.i]
		d.i++
	}
	return &repairTx{affected: n}, nil
}

func TestRepairZeroCheckpointsPoolRequiresPool(t *testing.T) {
	if _, err := RepairZeroCheckpointsPool(t.Context(), nil, CheckpointRepairOptions{BatchSize: 1}); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestRepairBatchErrorsSurface(t *testing.T) {
	if _, err := RepairZeroCheckpoints(t.Context(), &repairDB{beginErr: errors.New("begin")}, CheckpointRepairOptions{BatchSize: 1}); err == nil {
		t.Fatal("begin error ignored")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &failingExecRepairDB{fail: "last_seen_at"}, CheckpointRepairOptions{BatchSize: 1}); err == nil {
		t.Fatal("photo exec error ignored")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &failingExecRepairDB{fail: "last_fave_at", skipPhotos: true}, CheckpointRepairOptions{BatchSize: 1}); err == nil {
		t.Fatal("favorite exec error ignored")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &failingExecRepairDB{commitFail: true}, CheckpointRepairOptions{BatchSize: 1}); err == nil {
		t.Fatal("commit error ignored")
	}
	if _, err := RepairZeroCheckpoints(t.Context(), &failingExecRepairDB{commitFail: true, skipPhotos: true}, CheckpointRepairOptions{BatchSize: 1}); err == nil {
		t.Fatal("favorite commit error ignored")
	}
}

type failingExecRepairDB struct {
	fail       string
	skipPhotos bool
	commitFail bool
	photosDone bool
}

func (d *failingExecRepairDB) Begin(context.Context) (CheckpointRepairTx, error) {
	if d.skipPhotos && !d.photosDone {
		d.photosDone = true
		return &repairTx{affected: 0}, nil
	}
	tx := &repairTx{failExec: d.fail}
	if d.commitFail {
		tx.commitErr = errors.New("commit failed")
	}
	return tx, nil
}
