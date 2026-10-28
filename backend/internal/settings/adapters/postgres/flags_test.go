package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/settings/domain"
)

type flagDBStub struct {
	rows     flagRows
	queryErr error
	row      flagRow
	beginErr error
	tx       flagTx
	queries  int
	rowCalls int
	begins   int
}

func (database *flagDBStub) Query(context.Context, string, ...any) (flagRows, error) {
	database.queries++
	return database.rows, database.queryErr
}

func (database *flagDBStub) QueryRow(context.Context, string, ...any) flagRow {
	database.rowCalls++
	return database.row
}

func (database *flagDBStub) Begin(context.Context) (flagTx, error) {
	database.begins++
	return database.tx, database.beginErr
}

type flagRowsStub struct {
	values  [][]any
	index   int
	scanErr error
	rowsErr error
	closed  bool
}

func (rows *flagRowsStub) Next() bool { return rows.index < len(rows.values) }

func (rows *flagRowsStub) Scan(destinations ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	values := rows.values[rows.index]
	if len(values) != len(destinations) {
		return errors.New("wrong scan destination count")
	}
	for i, value := range values {
		switch target := destinations[i].(type) {
		case *string:
			*target = value.(string)
		case *bool:
			*target = value.(bool)
		case *[]int64:
			if value == nil {
				*target = nil
			} else {
				*target = append([]int64(nil), value.([]int64)...)
			}
		default:
			return errors.New("unexpected scan destination")
		}
	}
	rows.index++
	return nil
}

func (rows *flagRowsStub) Err() error { return rows.rowsErr }
func (rows *flagRowsStub) Close()     { rows.closed = true }

type flagRowStub struct {
	value bool
	err   error
}

func (row flagRowStub) Scan(destination ...any) error {
	if row.err != nil {
		return row.err
	}
	*destination[0].(*bool) = row.value
	return nil
}

type flagTxStub struct {
	execErrAt int
	commitErr error
	execCalls int
	queries   []string
	args      [][]any
	commits   int
	rollbacks int
}

func (tx *flagTxStub) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, append([]any(nil), args...))
	if tx.execCalls == tx.execErrAt {
		return pgconn.CommandTag{}, errors.New("exec failed")
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *flagTxStub) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

func (tx *flagTxStub) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

func TestNewFlagRepositoryAndReadiness(t *testing.T) {
	if _, err := NewFlagRepository(nil); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("NewFlagRepository(nil) error = %v", err)
	}
	if _, err := (*FlagRepository)(nil).ListFlags(t.Context()); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("nil repository ListFlags() error = %v", err)
	}
	repository := newFlagRepository(&flagDBStub{})
	if _, err := repository.ListFlags(nil); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("ListFlags(nil) error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.ListFlags(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListFlags(canceled) error = %v", err)
	}
	if _, err := repository.UserIsAdmin(nil, 1); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("UserIsAdmin(nil) error = %v", err)
	}
	if err := repository.UpsertFlags(nil, nil); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("UpsertFlags(nil) error = %v", err)
	}
}

func TestFlagRepositoryListFlags(t *testing.T) {
	rows := &flagRowsStub{values: [][]any{
		{"alpha", true, "all", nil},
		{"beta", false, "user_ids", []int64{2, 9}},
	}}
	database := &flagDBStub{rows: rows}
	repository := newFlagRepository(database)
	flags, err := repository.ListFlags(t.Context())
	if err != nil || len(flags) != 2 || flags[0].Audience != domain.AudienceAll || flags[0].UserIDs == nil || flags[1].Audience != domain.AudienceUserIDs || !reflect.DeepEqual(flags[1].UserIDs, []int64{2, 9}) {
		t.Fatalf("ListFlags() = %+v, %v", flags, err)
	}
	if !rows.closed || database.queries != 1 || !strings.Contains(listFeatureFlagsSQL, "ORDER BY name") {
		t.Fatalf("rows/query = %+v/%d", rows, database.queries)
	}

	for _, test := range []struct {
		name string
		rows *flagRowsStub
		err  error
		want string
	}{
		{name: "query", rows: &flagRowsStub{}, err: errors.New("query failed"), want: "query feature flags"},
		{name: "scan", rows: &flagRowsStub{values: [][]any{{"alpha", true, "all", nil}}, scanErr: errors.New("scan failed")}, want: "read feature flag"},
		{name: "iterate", rows: &flagRowsStub{rowsErr: errors.New("rows failed")}, want: "iterate feature flags"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := newFlagRepository(&flagDBStub{rows: test.rows, queryErr: test.err}).ListFlags(t.Context())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ListFlags() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestFlagRepositoryUserIsAdmin(t *testing.T) {
	database := &flagDBStub{row: flagRowStub{value: true}}
	repository := newFlagRepository(database)
	got, err := repository.UserIsAdmin(t.Context(), 7)
	if err != nil || !got || database.rowCalls != 1 || !strings.Contains(userIsAdminSQL, "disabled_at IS NULL") {
		t.Fatalf("UserIsAdmin() = %v, %v, row calls %d", got, err, database.rowCalls)
	}
	if got, err := repository.UserIsAdmin(t.Context(), 0); err != nil || got || database.rowCalls != 1 {
		t.Fatalf("UserIsAdmin(system) = %v, %v, row calls %d", got, err, database.rowCalls)
	}
	database.row = flagRowStub{err: errors.New("query failed")}
	if _, err := repository.UserIsAdmin(t.Context(), 7); err == nil || !strings.Contains(err.Error(), "query administrator role") {
		t.Fatalf("UserIsAdmin() error = %v", err)
	}
}

func TestFlagRepositoryUpsertFlagsIsAtomic(t *testing.T) {
	tx := &flagTxStub{}
	database := &flagDBStub{tx: tx}
	repository := newFlagRepository(database)
	flags := []domain.FeatureFlag{
		{Name: "alpha", Enabled: true, Audience: domain.AudienceAll},
		{Name: "targeted", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{2, 4}},
	}
	if err := repository.UpsertFlags(t.Context(), flags); err != nil {
		t.Fatalf("UpsertFlags() error = %v", err)
	}
	if database.begins != 1 || tx.execCalls != 2 || tx.commits != 1 || !strings.Contains(tx.queries[0], "ON CONFLICT") || tx.args[1][3] == nil {
		t.Fatalf("begin/tx = %d/%+v", database.begins, tx)
	}

	for _, test := range []struct {
		name string
		db   *flagDBStub
		list []domain.FeatureFlag
		want string
	}{
		{name: "empty", db: &flagDBStub{tx: &flagTxStub{}}, want: "at least one"},
		{name: "invalid", db: &flagDBStub{tx: &flagTxStub{}}, list: []domain.FeatureFlag{{Name: "INVALID", Audience: domain.AudienceAll}}, want: "invalid feature flag"},
		{name: "begin", db: &flagDBStub{beginErr: errors.New("begin failed")}, list: flags, want: "begin feature flag update"},
		{name: "exec", db: &flagDBStub{tx: &flagTxStub{execErrAt: 2}}, list: flags, want: "save feature flag"},
		{name: "commit", db: &flagDBStub{tx: &flagTxStub{commitErr: errors.New("commit failed")}}, list: flags, want: "commit feature flag update"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := newFlagRepository(test.db).UpsertFlags(t.Context(), test.list)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("UpsertFlags() error = %v, want %q", err, test.want)
			}
		})
	}
}
