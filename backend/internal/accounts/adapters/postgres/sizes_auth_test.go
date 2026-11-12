package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type sizesAuthRows struct {
	rows []SizesAuthCandidate
	i    int
	err  error
}

func (r *sizesAuthRows) Close()                                       {}
func (r *sizesAuthRows) Err() error                                   { return r.err }
func (r *sizesAuthRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *sizesAuthRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *sizesAuthRows) Next() bool {
	if r.i >= len(r.rows) {
		return false
	}
	r.i++
	return true
}

func (r *sizesAuthRows) Scan(dest ...any) error {
	row := r.rows[r.i-1]
	*dest[0].(*int64), *dest[1].(*int64), *dest[2].(*int64), *dest[3].(*string) = row.AccountID, row.UserID, row.KeyID, row.NSID
	return nil
}
func (r *sizesAuthRows) Values() ([]any, error) { return nil, nil }
func (r *sizesAuthRows) RawValues() [][]byte    { return nil }
func (r *sizesAuthRows) Conn() *pgx.Conn        { return nil }
func (r *sizesAuthRows) TypeMap() *pgtype.Map   { return nil }

type sizesAuthDBFake struct {
	rows     *sizesAuthRows
	queryErr error
	query    string
	owner    string
	userID   int64
}

func (d *sizesAuthDBFake) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	if d.queryErr != nil {
		return nil, d.queryErr
	}
	d.query = query
	d.owner, _ = args[0].(string)
	d.userID, _ = args[1].(int64)
	return d.rows, nil
}

func TestSizesAuthCandidatesOrdersOwnerFirst(t *testing.T) {
	db := &sizesAuthDBFake{rows: &sizesAuthRows{rows: []SizesAuthCandidate{
		{AccountID: 1, UserID: 7, KeyID: 4, NSID: "owner@N00"},
		{AccountID: 2, UserID: 7, KeyID: 5, NSID: "other@N00"},
	}}}
	got, err := (SizesAuthCandidates{DB: db}).List(t.Context(), "owner@N00", 7)
	if err != nil || len(got) != 2 || got[0].AccountID != 1 || got[1].AccountID != 2 {
		t.Fatalf("List() = %+v, %v", got, err)
	}
	if db.owner != "owner@N00" || db.userID != 7 {
		t.Fatalf("query args = %q %d", db.owner, db.userID)
	}
	if !strings.Contains(db.query, "a.user_id = $2") || !strings.Contains(db.query, "k.status = 'valid'") {
		t.Fatalf("candidate query is not scoped to the requester and valid keys: %s", db.query)
	}
}

func TestSizesAuthCandidatesRequiresDependencies(t *testing.T) {
	if _, err := (SizesAuthCandidates{}).List(t.Context(), "owner@N00", 7); err == nil {
		t.Fatal("nil db succeeded")
	}
	db := &sizesAuthDBFake{queryErr: errors.New("db down")}
	if _, err := (SizesAuthCandidates{DB: db}).List(t.Context(), "owner@N00", 7); !errors.Is(err, db.queryErr) {
		t.Fatalf("query error = %v", err)
	}
}
