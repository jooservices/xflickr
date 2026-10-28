package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
)

func TestRepositoryCRUDMethods(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if New(&pgxpool.Pool{}) == nil {
		t.Fatal("New returned nil")
	}
	if exists, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{values: []any{true}}}}).UsersExist(ctx); err != nil || !exists {
		t.Fatalf("UsersExist=%v err=%v", exists, err)
	}
	boom := errors.New("database failed")
	if _, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: boom}}}).UsersExist(ctx); !errors.Is(err, boom) {
		t.Fatalf("UsersExist error=%v", err)
	}

	for _, find := range []struct {
		name string
		call func(*Repository) (domain.User, error)
	}{
		{"email", func(repo *Repository) (domain.User, error) { return repo.FindByEmail(ctx, "a@example.test") }},
		{"id", func(repo *Repository) (domain.User, error) { return repo.FindByID(ctx, 4) }},
	} {
		t.Run(find.name, func(t *testing.T) {
			values := []any{int64(4), "a@example.test", "hash", domain.RoleUser, "en", now, (*string)(nil)}
			user, err := find.call(testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{values: values}}}))
			if err != nil || user.ID != 4 || user.Disabled {
				t.Fatalf("found user=%+v err=%v", user, err)
			}
			values[6] = ptrString("disabled")
			user, err = find.call(testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{values: values}}}))
			if err != nil || !user.Disabled {
				t.Fatalf("disabled user=%+v err=%v", user, err)
			}
			if _, err := find.call(testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: pgx.ErrNoRows}}})); !errors.Is(err, app.ErrUserNotFound) {
				t.Fatalf("missing user error=%v", err)
			}
			if _, err := find.call(testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: boom}}})); !errors.Is(err, boom) {
				t.Fatalf("query error=%v", err)
			}
		})
	}

	localeValues := []any{int64(4), "a@example.test", domain.RoleUser, "fr", now}
	updated, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{values: localeValues}}}).UpdateLocale(ctx, 4, "fr")
	if err != nil || updated.Locale != "fr" {
		t.Fatalf("UpdateLocale=%+v err=%v", updated, err)
	}
	if _, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: pgx.ErrNoRows}}}).UpdateLocale(ctx, 4, "fr"); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("missing locale user error=%v", err)
	}
	if _, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: boom}}}).UpdateLocale(ctx, 4, "fr"); !errors.Is(err, boom) {
		t.Fatalf("locale query error=%v", err)
	}
	if err := testRepo(&identityPoolFake{execTag: pgconn.NewCommandTag("UPDATE 1")}).UpdatePassword(ctx, 4, "hash"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	if err := testRepo(&identityPoolFake{execTag: pgconn.NewCommandTag("UPDATE 0")}).UpdatePassword(ctx, 4, "hash"); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("missing password user error=%v", err)
	}
	if err := testRepo(&identityPoolFake{execErr: boom}).UpdatePassword(ctx, 4, "hash"); !errors.Is(err, boom) {
		t.Fatalf("password query error=%v", err)
	}
}

func TestRegisterSerializesFirstUserAndMapsFailures(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	user := domain.User{Email: "a@example.test", PasswordHash: "hash", Locale: "en"}
	adminRow := identityRow{values: []any{int64(1), user.Email, domain.RoleAdmin, user.Locale, now}}
	regularRow := identityRow{values: []any{int64(2), user.Email, domain.RoleUser, user.Locale, now}}
	for _, tc := range []struct {
		name  string
		count int
		open  bool
		want  domain.Role
	}{
		{"first admin", 0, false, domain.RoleAdmin},
		{"open registration user", 3, true, domain.RoleUser},
	} {
		t.Run(tc.name, func(t *testing.T) {
			insert := adminRow
			if tc.want == domain.RoleUser {
				insert = regularRow
			}
			tx := &identityTxFake{rowQueue: []pgx.Row{identityRow{values: []any{tc.count}}, insert}}
			got, err := testRepo(&identityPoolFake{tx: tx}).Register(ctx, user, tc.open)
			if err != nil || got.Role != tc.want || !tx.committed {
				t.Fatalf("Register=%+v err=%v committed=%v", got, err, tx.committed)
			}
		})
	}
	closed := &identityTxFake{rowQueue: []pgx.Row{identityRow{values: []any{1}}}}
	if _, err := testRepo(&identityPoolFake{tx: closed}).Register(ctx, user, false); !errors.Is(err, app.ErrRegistrationClosed) {
		t.Fatalf("closed registration error=%v", err)
	}
	boom := errors.New("database failed")
	if _, err := testRepo(&identityPoolFake{beginErr: boom}).Register(ctx, user, true); !errors.Is(err, boom) {
		t.Fatalf("begin error=%v", err)
	}
	if _, err := testRepo(&identityPoolFake{tx: &identityTxFake{execErr: boom}}).Register(ctx, user, true); !errors.Is(err, boom) {
		t.Fatalf("lock error=%v", err)
	}
	if _, err := testRepo(&identityPoolFake{tx: &identityTxFake{rowQueue: []pgx.Row{identityRow{err: boom}}}}).Register(ctx, user, true); !errors.Is(err, boom) {
		t.Fatalf("count error=%v", err)
	}
	tx := &identityTxFake{rowQueue: []pgx.Row{identityRow{values: []any{0}}, identityRow{err: &pgconn.PgError{Code: "23505"}}}}
	if _, err := testRepo(&identityPoolFake{tx: tx}).Register(ctx, user, true); !errors.Is(err, app.ErrEmailExists) {
		t.Fatalf("duplicate email error=%v", err)
	}
	tx = &identityTxFake{rowQueue: []pgx.Row{identityRow{values: []any{0}}, identityRow{err: boom}}}
	if _, err := testRepo(&identityPoolFake{tx: tx}).Register(ctx, user, true); !errors.Is(err, boom) {
		t.Fatalf("insert error=%v", err)
	}
	tx = &identityTxFake{rowQueue: []pgx.Row{identityRow{values: []any{0}}, adminRow}, commitErr: boom}
	if _, err := testRepo(&identityPoolFake{tx: tx}).Register(ctx, user, true); !errors.Is(err, boom) {
		t.Fatalf("commit error=%v", err)
	}
}

func TestListUsersEscapesSearchAndReportsFailures(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	queryRows := func(rows pgx.Rows, err error) *identityPoolFake {
		return &identityPoolFake{rowQueue: []pgx.Row{identityRow{values: []any{int64(1)}}}, rows: rows, queryErr: err}
	}
	values := []any{int64(4), "a_b%@example.test", "hash", domain.RoleUser, "en", now, false}
	pool := queryRows(&identityRowsFake{values: [][]any{values}}, nil)
	users, total, err := testRepo(pool).ListUsers(ctx, "a_b%", 10, 2)
	if err != nil || total != 1 || len(users) != 1 || users[0].Email != values[1] {
		t.Fatalf("ListUsers=%+v total=%d err=%v", users, total, err)
	}
	if !strings.Contains(pool.query, "ORDER BY created_at DESC") || pool.args[0] != "%a\\_b\\%%" {
		t.Fatalf("query=%q args=%v", pool.query, pool.args)
	}
	boom := errors.New("database failed")
	if _, _, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: boom}}}).ListUsers(ctx, "", 10, 0); !errors.Is(err, boom) {
		t.Fatalf("count error=%v", err)
	}
	if _, _, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{values: []any{int64(1)}}}, queryErr: boom}).ListUsers(ctx, "", 10, 0); !errors.Is(err, boom) {
		t.Fatalf("list error=%v", err)
	}
	if _, _, err := testRepo(queryRows(&identityRowsFake{values: [][]any{values}, scanErr: boom}, nil)).ListUsers(ctx, "", 10, 0); !errors.Is(err, boom) {
		t.Fatalf("scan error=%v", err)
	}
	if _, _, err := testRepo(queryRows(&identityRowsFake{rowErr: boom}, nil)).ListUsers(ctx, "", 10, 0); !errors.Is(err, boom) {
		t.Fatalf("rows error=%v", err)
	}
}

func TestUpdateUserTransactionAndFailurePaths(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	beforeValues := []any{int64(4), "a@example.test", "hash", domain.RoleAdmin, "en", now, false}
	afterValues := []any{int64(4), "a@example.test", "hash", domain.RoleUser, "en", now, false}
	decide := func(user domain.User, _ int) (domain.User, error) { user.Role = domain.RoleUser; return user, nil }
	tx := &identityTxFake{rowQueue: []pgx.Row{
		identityRow{values: beforeValues}, identityRow{values: []any{2}}, identityRow{values: afterValues},
	}}
	before, after, err := testRepo(&identityPoolFake{tx: tx}).UpdateUser(ctx, 4, decide)
	if err != nil || before.Role != domain.RoleAdmin || after.Role != domain.RoleUser || !tx.committed {
		t.Fatalf("UpdateUser before=%+v after=%+v err=%v", before, after, err)
	}
	boom := errors.New("database failed")
	if _, _, err := testRepo(&identityPoolFake{beginErr: boom}).UpdateUser(ctx, 4, decide); !errors.Is(err, boom) {
		t.Fatalf("begin error=%v", err)
	}
	if _, _, err := testRepo(&identityPoolFake{tx: &identityTxFake{execErr: boom}}).UpdateUser(ctx, 4, decide); !errors.Is(err, boom) {
		t.Fatalf("lock error=%v", err)
	}
	if _, _, err := testRepo(&identityPoolFake{tx: &identityTxFake{rowQueue: []pgx.Row{identityRow{err: pgx.ErrNoRows}}}}).UpdateUser(ctx, 4, decide); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("missing user error=%v", err)
	}
	if _, _, err := testRepo(&identityPoolFake{tx: &identityTxFake{rowQueue: []pgx.Row{identityRow{err: boom}}}}).UpdateUser(ctx, 4, decide); !errors.Is(err, boom) {
		t.Fatalf("load user error=%v", err)
	}
	countFail := &identityTxFake{rowQueue: []pgx.Row{identityRow{values: beforeValues}, identityRow{err: boom}}}
	if _, _, err := testRepo(&identityPoolFake{tx: countFail}).UpdateUser(ctx, 4, decide); !errors.Is(err, boom) {
		t.Fatalf("count admins error=%v", err)
	}
	decisionErr := errors.New("decision rejected")
	decideFail := func(domain.User, int) (domain.User, error) { return domain.User{}, decisionErr }
	decideTx := &identityTxFake{rowQueue: []pgx.Row{identityRow{values: beforeValues}, identityRow{values: []any{2}}}}
	if _, _, err := testRepo(&identityPoolFake{tx: decideTx}).UpdateUser(ctx, 4, decideFail); !errors.Is(err, decisionErr) {
		t.Fatalf("decision error=%v", err)
	}
	updateFail := &identityTxFake{rowQueue: []pgx.Row{identityRow{values: beforeValues}, identityRow{values: []any{2}}, identityRow{err: boom}}}
	if _, _, err := testRepo(&identityPoolFake{tx: updateFail}).UpdateUser(ctx, 4, decide); !errors.Is(err, boom) {
		t.Fatalf("update error=%v", err)
	}
	commitFail := &identityTxFake{rowQueue: []pgx.Row{identityRow{values: beforeValues}, identityRow{values: []any{2}}, identityRow{values: afterValues}}, commitErr: boom}
	if _, _, err := testRepo(&identityPoolFake{tx: commitFail}).UpdateUser(ctx, 4, decide); !errors.Is(err, boom) {
		t.Fatalf("commit error=%v", err)
	}
}

func TestSessionRepositoryMethodsAndErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	sessionValues := []any{int64(8), int64(4), now.Add(time.Hour), now, now, "agent", "127.0.0.1"}
	if session, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{values: sessionValues}}}).InsertSession(ctx, 4, []byte{1}, now.Add(time.Hour), now, "agent", "127.0.0.1"); err != nil || session.ID != 8 {
		t.Fatalf("InsertSession=%+v err=%v", session, err)
	}
	boom := errors.New("database failed")
	if _, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: boom}}}).InsertSession(ctx, 4, nil, now, now, "", ""); !errors.Is(err, boom) {
		t.Fatalf("InsertSession error=%v", err)
	}
	if session, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{values: sessionValues}}}).FindSessionByHash(ctx, []byte{1}, now); err != nil || session.ID != 8 {
		t.Fatalf("FindSessionByHash=%+v err=%v", session, err)
	}
	if _, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: pgx.ErrNoRows}}}).FindSessionByHash(ctx, nil, now); !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatalf("missing session error=%v", err)
	}
	if _, err := testRepo(&identityPoolFake{rowQueue: []pgx.Row{identityRow{err: boom}}}).FindSessionByHash(ctx, nil, now); !errors.Is(err, boom) {
		t.Fatalf("session query error=%v", err)
	}
	for _, affected := range []int64{0, 1} {
		got, err := testRepo(&identityPoolFake{execTag: pgconn.NewCommandTag("UPDATE " + fmt.Sprint(affected))}).TouchSession(ctx, nil, now, now)
		if err != nil || got != (affected == 1) {
			t.Fatalf("TouchSession affected=%d got=%v err=%v", affected, got, err)
		}
	}
	if _, err := testRepo(&identityPoolFake{execErr: boom}).TouchSession(ctx, nil, now, now); !errors.Is(err, boom) {
		t.Fatalf("TouchSession error=%v", err)
	}
	for _, action := range []func(*Repository) error{
		func(repo *Repository) error { return repo.DeleteOtherSessions(ctx, 4, nil) },
		func(repo *Repository) error { return repo.DeleteSessionByHash(ctx, nil) },
	} {
		if err := action(testRepo(&identityPoolFake{})); err != nil {
			t.Fatalf("delete session: %v", err)
		}
		if err := action(testRepo(&identityPoolFake{execErr: boom})); !errors.Is(err, boom) {
			t.Fatalf("delete error=%v", err)
		}
	}
	rows := &identityRowsFake{values: [][]any{sessionValues}}
	sessions, err := testRepo(&identityPoolFake{rows: rows}).ListSessions(ctx, 4)
	if err != nil || len(sessions) != 1 || sessions[0].ID != 8 {
		t.Fatalf("ListSessions=%+v err=%v", sessions, err)
	}
	if _, err := testRepo(&identityPoolFake{queryErr: boom}).ListSessions(ctx, 4); !errors.Is(err, boom) {
		t.Fatalf("ListSessions query error=%v", err)
	}
	if _, err := testRepo(&identityPoolFake{rows: &identityRowsFake{values: [][]any{sessionValues}, scanErr: boom}}).ListSessions(ctx, 4); !errors.Is(err, boom) {
		t.Fatalf("ListSessions scan error=%v", err)
	}
	if _, err := testRepo(&identityPoolFake{rows: &identityRowsFake{rowErr: boom}}).ListSessions(ctx, 4); !errors.Is(err, boom) {
		t.Fatalf("ListSessions rows error=%v", err)
	}
	for _, affected := range []int64{0, 1} {
		got, err := testRepo(&identityPoolFake{execTag: pgconn.NewCommandTag("DELETE " + fmt.Sprint(affected))}).DeleteSessionByID(ctx, 4, 8)
		if err != nil || got != (affected > 0) {
			t.Fatalf("DeleteSessionByID affected=%d got=%v err=%v", affected, got, err)
		}
	}
	if _, err := testRepo(&identityPoolFake{execErr: boom}).DeleteSessionByID(ctx, 4, 8); !errors.Is(err, boom) {
		t.Fatalf("DeleteSessionByID error=%v", err)
	}
}

func testRepo(pool repositoryPool) *Repository { return &Repository{pool: pool} }

func ptrString(value string) *string { return &value }

type identityPoolFake struct {
	rowQueue []pgx.Row
	rows     pgx.Rows
	query    string
	args     []any
	queryErr error
	execTag  pgconn.CommandTag
	execErr  error
	tx       pgx.Tx
	beginErr error
}

func (pool *identityPoolFake) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	pool.query, pool.args = query, append([]any(nil), args...)
	if len(pool.rowQueue) == 0 {
		return identityRow{err: errors.New("unexpected QueryRow")}
	}
	row := pool.rowQueue[0]
	pool.rowQueue = pool.rowQueue[1:]
	return row
}

func (pool *identityPoolFake) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	pool.query, pool.args = query, append([]any(nil), args...)
	if pool.queryErr != nil {
		return nil, pool.queryErr
	}
	return pool.rows, nil
}

func (pool *identityPoolFake) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pool.execTag, pool.execErr
}
func (pool *identityPoolFake) Begin(context.Context) (pgx.Tx, error) { return pool.begin() }
func (pool *identityPoolFake) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return pool.begin()
}

func (pool *identityPoolFake) begin() (pgx.Tx, error) {
	if pool.beginErr != nil {
		return nil, pool.beginErr
	}
	if pool.tx == nil {
		pool.tx = &identityTxFake{}
	}
	return pool.tx, nil
}

type identityTxFake struct {
	pgx.Tx
	rowQueue  []pgx.Row
	execErr   error
	commitErr error
	committed bool
}

func (tx *identityTxFake) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), tx.execErr
}

func (tx *identityTxFake) QueryRow(context.Context, string, ...any) pgx.Row {
	if len(tx.rowQueue) == 0 {
		return identityRow{err: errors.New("unexpected transaction QueryRow")}
	}
	row := tx.rowQueue[0]
	tx.rowQueue = tx.rowQueue[1:]
	return row
}
func (tx *identityTxFake) Commit(context.Context) error { tx.committed = true; return tx.commitErr }
func (*identityTxFake) Rollback(context.Context) error  { return nil }

type identityRow struct {
	values []any
	err    error
}

func (row identityRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("unexpected scan value count")
	}
	for i := range dest {
		out, value := reflect.ValueOf(dest[i]), reflect.ValueOf(row.values[i])
		if out.Kind() != reflect.Pointer {
			return errors.New("scan destination is not a pointer")
		}
		switch {
		case !value.IsValid():
			out.Elem().SetZero()
		case value.Type().AssignableTo(out.Elem().Type()):
			out.Elem().Set(value)
		default:
			return errors.New("unexpected scan value type")
		}
	}
	return nil
}

type identityRowsFake struct {
	pgx.Rows
	values  [][]any
	index   int
	scanErr error
	rowErr  error
}

func (rows *identityRowsFake) Next() bool {
	if rows.index >= len(rows.values) {
		return false
	}
	rows.index++
	return true
}
func (*identityRowsFake) Close()          {}
func (rows *identityRowsFake) Err() error { return rows.rowErr }
func (rows *identityRowsFake) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	return (identityRow{values: rows.values[rows.index-1]}).Scan(dest...)
}
