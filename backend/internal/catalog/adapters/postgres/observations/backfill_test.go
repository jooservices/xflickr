package observations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestBackfillRejectsInvalidOptions(t *testing.T) {
	for _, options := range []BackfillOptions{{}, {UserID: 1, AccountID: 2}, {UserID: 1, AccountID: 2, BatchSize: 10001}} {
		if _, err := Backfill(context.Background(), nil, options); err == nil {
			t.Fatal("invalid backfill accepted")
		}
	}
}

type backfillStub struct {
	fail    string
	batches int
	queries int
	rolled  int
}
type backfillTxStub struct {
	pgx.Tx
	db *backfillStub
}
type backfillRow func(...any) error

func (r backfillRow) Scan(dest ...any) error { return r(dest...) }
func (s *backfillStub) Begin(context.Context) (pgx.Tx, error) {
	if s.fail == "begin" {
		return nil, errors.New("begin")
	}
	s.batches++
	return &backfillTxStub{db: s}, nil
}

func (s *backfillTxStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if s.db.fail == "lock" {
		return pgconn.CommandTag{}, errors.New("lock")
	}
	return pgconn.NewCommandTag("LOCK TABLE"), nil
}
func (s *backfillTxStub) Rollback(context.Context) error { s.db.rolled++; return nil }
func (s *backfillTxStub) Commit(context.Context) error {
	if s.db.fail == "commit" {
		return errors.New("commit")
	}
	return nil
}

func (s *backfillTxStub) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return backfillRow(func(dest ...any) error {
		stage := "page"
		if strings.Contains(sql, "FROM users)") {
			stage = "guard"
		} else if strings.Contains(sql, "SELECT (SELECT count(*) FROM people)") {
			stage = "counts"
		}
		if s.db.fail == stage {
			return errors.New(stage)
		}
		switch stage {
		case "guard":
			*dest[0].(*int64) = 1
			*dest[1].(*int64) = 1
			*dest[2].(*bool) = true
			if s.db.fail == "refuse" {
				*dest[0].(*int64) = 2
			}
		case "counts":
			*dest[0].(*int64) = 2
			*dest[1].(*int64) = 2
		default:
			s.db.queries++
			if s.db.queries == 1 || s.db.queries == 3 {
				if args[2] == "person" {
					*dest[0].(*string) = "p"
				} else {
					*dest[0].(*string) = "2"
				}
				*dest[1].(*int64) = 2
			}
		}
		return nil
	})
}

func TestBackfillBatchesAndStorageErrors(t *testing.T) {
	options := BackfillOptions{UserID: 1, AccountID: 2, BatchSize: 2}
	stub := &backfillStub{}
	report, err := Backfill(t.Context(), stub, options)
	if err != nil || report.Inserted != 4 || report.Batches != 4 || stub.rolled != 4 {
		t.Fatalf("report=%+v stub=%+v err=%v", report, stub, err)
	}
	for _, fail := range []string{"begin", "lock", "guard", "counts", "page", "commit", "refuse"} {
		t.Run(fail, func(t *testing.T) {
			stub := &backfillStub{fail: fail}
			report, err := Backfill(t.Context(), stub, options)
			if err == nil || report.Inserted != 0 {
				t.Fatalf("failure=%s report=%+v err=%v", fail, report, err)
			}
		})
	}
}
