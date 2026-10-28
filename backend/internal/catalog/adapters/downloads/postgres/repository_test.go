package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDownloadCandidatesReadsKindsAndDeduplicatesObservers(t *testing.T) {
	ctx := context.Background()
	observerID := int64(7)
	rows := &candidateRows{values: [][]any{
		{int64(1), "owner", "public", true, (*int64)(nil), false},
		{int64(1), "owner", "public", true, &observerID, true},
		{int64(2), "owner", "private", false, (*int64)(nil), false},
	}}
	db := &candidateDB{rows: rows}
	for _, kind := range []string{"contact", "album", "tray"} {
		rows.position = 0
		got, err := (Repository{Pool: db}).DownloadCandidates(ctx, kind, "owner", []int64{1, 2})
		if err != nil || len(got) != 2 {
			t.Fatalf("%s candidates=%+v err=%v", kind, got, err)
		}
		if got[0].PhotoID != 1 || !got[0].SizesConfirmed || len(got[0].Observers) != 1 || got[0].Observers[0].UserID != observerID || !got[0].Observers[0].SharingEnabled {
			t.Fatalf("%s first candidate=%+v", kind, got[0])
		}
		if got[1].PhotoID != 2 || got[1].Visibility != "private" || got[1].SizesConfirmed {
			t.Fatalf("%s second candidate=%+v", kind, got[1])
		}
		if !strings.Contains(db.query, "SELECT") {
			t.Fatalf("%s query=%q", kind, db.query)
		}
	}
}

func TestDownloadCandidatesValidatesAndReportsDatabaseErrors(t *testing.T) {
	ctx := context.Background()
	db := &candidateDB{rows: &candidateRows{}}
	if _, err := (Repository{}).DownloadCandidates(ctx, "contact", "owner", nil); err == nil {
		t.Fatal("missing pool was accepted")
	}
	if _, err := (Repository{Pool: db}).DownloadCandidates(nil, "contact", "owner", nil); err == nil {
		t.Fatal("missing context was accepted")
	}
	if _, err := (Repository{Pool: db}).DownloadCandidates(ctx, "unknown", "owner", nil); err == nil {
		t.Fatal("invalid kind was accepted")
	}
	boom := errors.New("database failed")
	db.queryErr = boom
	if _, err := (Repository{Pool: db}).DownloadCandidates(ctx, "contact", "owner", nil); !errors.Is(err, boom) {
		t.Fatalf("query error=%v", err)
	}
	db.queryErr = nil
	db.rows = &candidateRows{values: [][]any{{int64(1), "owner", "public", true, (*int64)(nil), false}}, scanErr: boom}
	if _, err := (Repository{Pool: db}).DownloadCandidates(ctx, "contact", "owner", nil); !errors.Is(err, boom) {
		t.Fatalf("scan error=%v", err)
	}
	db.rows = &candidateRows{rowErr: boom}
	if _, err := (Repository{Pool: db}).DownloadCandidates(ctx, "contact", "owner", nil); !errors.Is(err, boom) {
		t.Fatalf("rows error=%v", err)
	}
}

type candidateDB struct {
	query    string
	args     []any
	rows     pgx.Rows
	queryErr error
}

func (db *candidateDB) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	db.query, db.args = query, append([]any(nil), args...)
	if db.queryErr != nil {
		return nil, db.queryErr
	}
	return db.rows, nil
}

type candidateRows struct {
	pgx.Rows
	values   [][]any
	position int
	scanErr  error
	rowErr   error
}

func (rows *candidateRows) Next() bool {
	if rows.position >= len(rows.values) {
		return false
	}
	rows.position++
	return true
}
func (*candidateRows) Close()          {}
func (rows *candidateRows) Err() error { return rows.rowErr }
func (rows *candidateRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	values := rows.values[rows.position-1]
	if len(dest) != len(values) {
		return errors.New("unexpected scan value count")
	}
	for i := range dest {
		out, value := reflect.ValueOf(dest[i]), reflect.ValueOf(values[i])
		if out.Kind() != reflect.Pointer || !value.IsValid() || !value.Type().AssignableTo(out.Elem().Type()) {
			return errors.New("unexpected scan value type")
		}
		out.Elem().Set(value)
	}
	return nil
}
