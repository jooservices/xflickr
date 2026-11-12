package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/settings/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/settings/app"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

// fakeFlagQueries records the calls the repository makes to the generated queries.
type fakeFlagQueries struct {
	list       []gen.ListFeatureFlagsRow
	listErr    error
	row        gen.GetFeatureFlagRow
	getErr     error
	admin      bool
	adminErr   error
	upsertErr  error
	failAt     int // 1-based upsert call that returns upsertErr
	listCalls  int
	getName    string
	getCalls   int
	adminID    int64
	adminCalls int
	upserts    []gen.UpsertFeatureFlagParams
}

func (queries *fakeFlagQueries) ListFeatureFlags(context.Context) ([]gen.ListFeatureFlagsRow, error) {
	queries.listCalls++
	return queries.list, queries.listErr
}

func (queries *fakeFlagQueries) GetFeatureFlag(_ context.Context, name string) (gen.GetFeatureFlagRow, error) {
	queries.getCalls++
	queries.getName = name
	return queries.row, queries.getErr
}

func (queries *fakeFlagQueries) UserIsAdmin(_ context.Context, userID int64) (bool, error) {
	queries.adminCalls++
	queries.adminID = userID
	return queries.admin, queries.adminErr
}

func (queries *fakeFlagQueries) UpsertFeatureFlag(_ context.Context, arg gen.UpsertFeatureFlagParams) error {
	queries.upserts = append(queries.upserts, arg)
	if queries.upsertErr != nil && len(queries.upserts) == queries.failAt {
		return queries.upsertErr
	}
	return nil
}

// fakeFlagTx embeds a nil pgx.Tx: the fake queries ignore the transaction, so only Commit and Rollback run.
type fakeFlagTx struct {
	pgx.Tx
	commitErr error
	commits   int
	rollbacks int
}

func (tx *fakeFlagTx) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

func (tx *fakeFlagTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type fakeFlagTransactions struct {
	tx       *fakeFlagTx
	beginErr error
	begins   int
}

func (txs *fakeFlagTransactions) Begin(context.Context) (pgx.Tx, error) {
	txs.begins++
	if txs.beginErr != nil {
		return nil, txs.beginErr
	}
	return txs.tx, nil
}

// newTestFlagRepository runs the upserts on the same fake queries and records the transaction they were bound to.
func newTestFlagRepository(queries *fakeFlagQueries, txs *fakeFlagTransactions, bound *pgx.Tx) *FlagRepository {
	return newFlagRepository(queries, txs, func(tx pgx.Tx) flagQueries {
		*bound = tx
		return queries
	})
}

func TestNewFlagRepositoryAndReadiness(t *testing.T) {
	if _, err := NewFlagRepository(nil); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("NewFlagRepository(nil) error = %v", err)
	}
	if repository, err := NewFlagRepository(new(pgxpool.Pool)); err != nil || repository == nil {
		t.Fatalf("NewFlagRepository(pool) = %v, %v", repository, err)
	}
	if _, err := (*FlagRepository)(nil).ListFlags(t.Context()); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("nil repository ListFlags() error = %v", err)
	}
	if _, err := newFlagRepository(nil, nil, nil).GetFlag(t.Context(), "alpha"); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("repository without queries GetFlag() error = %v", err)
	}

	queries := &fakeFlagQueries{}
	repository := newTestFlagRepository(queries, &fakeFlagTransactions{}, new(pgx.Tx))
	if _, err := repository.ListFlags(nil); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("ListFlags(nil) error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.ListFlags(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListFlags(canceled) error = %v", err)
	}
	if queries.listCalls != 0 {
		t.Fatalf("canceled context reached the queries %d times", queries.listCalls)
	}
	if _, err := repository.UserIsAdmin(nil, 1); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("UserIsAdmin(nil) error = %v", err)
	}
	if _, err := repository.GetFlag(nil, "alpha"); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("GetFlag(nil) error = %v", err)
	}
	if err := repository.UpsertFlags(nil, nil); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("UpsertFlags(nil) error = %v", err)
	}
}

func TestFlagRepositoryListFlags(t *testing.T) {
	t.Run("maps rows in stored order and reads a NULL array as no users", func(t *testing.T) {
		queries := &fakeFlagQueries{list: []gen.ListFeatureFlagsRow{
			{Name: "alpha", Enabled: true, Audience: "all", UserIds: nil},
			{Name: "beta", Enabled: false, Audience: "user_ids", UserIds: []int64{2, 9}},
		}}
		flags, err := newTestFlagRepository(queries, &fakeFlagTransactions{}, new(pgx.Tx)).ListFlags(t.Context())
		want := []domain.FeatureFlag{
			{Name: "alpha", Enabled: true, Audience: domain.AudienceAll, UserIDs: []int64{}},
			{Name: "beta", Enabled: false, Audience: domain.AudienceUserIDs, UserIDs: []int64{2, 9}},
		}
		if err != nil || !reflect.DeepEqual(flags, want) || queries.listCalls != 1 {
			t.Fatalf("ListFlags() = %+v, %v, calls %d; want %+v", flags, err, queries.listCalls, want)
		}
	})

	t.Run("returns an empty list when no flag exists", func(t *testing.T) {
		flags, err := newTestFlagRepository(&fakeFlagQueries{}, &fakeFlagTransactions{}, new(pgx.Tx)).ListFlags(t.Context())
		if err != nil || flags == nil || len(flags) != 0 {
			t.Fatalf("ListFlags() = %#v, %v; want empty non-nil list", flags, err)
		}
	})

	t.Run("wraps a query failure", func(t *testing.T) {
		queryErr := errors.New("query failed")
		_, err := newTestFlagRepository(&fakeFlagQueries{listErr: queryErr}, &fakeFlagTransactions{}, new(pgx.Tx)).ListFlags(t.Context())
		if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "query feature flags") {
			t.Fatalf("ListFlags() error = %v", err)
		}
	})
}

func TestFlagRepositoryGetFlag(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	for _, test := range []struct {
		name     string
		row      gen.GetFeatureFlagRow
		rowErr   error
		want     domain.FeatureFlag
		wantErr  error
		wantText string
	}{
		{
			name: "all audience with NULL array",
			row:  gen.GetFeatureFlagRow{Name: "admin.banner", Enabled: false, Audience: "all"},
			want: domain.FeatureFlag{Name: "admin.banner", Enabled: false, Audience: domain.AudienceAll, UserIDs: []int64{}},
		},
		{
			name: "user id audience preserves stored order",
			row:  gen.GetFeatureFlagRow{Name: "admin.banner", Enabled: true, Audience: "user_ids", UserIds: []int64{9, 3}},
			want: domain.FeatureFlag{Name: "admin.banner", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{9, 3}},
		},
		{name: "not found", rowErr: pgx.ErrNoRows, wantErr: app.ErrFeatureFlagNotFound},
		{name: "database error", rowErr: databaseErr, wantErr: databaseErr, wantText: "query feature flag"},
	} {
		t.Run(test.name, func(t *testing.T) {
			queries := &fakeFlagQueries{row: test.row, getErr: test.rowErr}
			got, err := newTestFlagRepository(queries, &fakeFlagTransactions{}, new(pgx.Tx)).GetFlag(t.Context(), "admin.banner")
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("GetFlag() error = %v; want %v", err, test.wantErr)
				}
				if test.wantText != "" && !strings.Contains(err.Error(), test.wantText) {
					t.Fatalf("database error was not wrapped: %v", err)
				}
			} else if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("GetFlag() = %+v, %v; want %+v", got, err, test.want)
			}
			if queries.getCalls != 1 || queries.getName != "admin.banner" {
				t.Fatalf("GetFeatureFlag called %d times with %q", queries.getCalls, queries.getName)
			}
		})
	}
}

func TestFlagRepositoryUserIsAdmin(t *testing.T) {
	queries := &fakeFlagQueries{admin: true}
	repository := newTestFlagRepository(queries, &fakeFlagTransactions{}, new(pgx.Tx))
	got, err := repository.UserIsAdmin(t.Context(), 7)
	if err != nil || !got || queries.adminCalls != 1 || queries.adminID != 7 {
		t.Fatalf("UserIsAdmin() = %v, %v, calls %d for id %d", got, err, queries.adminCalls, queries.adminID)
	}
	if got, err := repository.UserIsAdmin(t.Context(), 0); err != nil || got || queries.adminCalls != 1 {
		t.Fatalf("UserIsAdmin(system) = %v, %v, calls %d; want false without a query", got, err, queries.adminCalls)
	}
	queries.adminErr = errors.New("query failed")
	if _, err := repository.UserIsAdmin(t.Context(), 7); err == nil || !strings.Contains(err.Error(), "query administrator role") {
		t.Fatalf("UserIsAdmin() error = %v", err)
	}
}

func TestFlagRepositoryUpsertFlags(t *testing.T) {
	flags := []domain.FeatureFlag{
		{Name: "alpha", Enabled: true, Audience: domain.AudienceAll},
		{Name: "targeted", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{2, 4}},
	}

	t.Run("saves every flag in one committed transaction", func(t *testing.T) {
		queries := &fakeFlagQueries{}
		txs := &fakeFlagTransactions{tx: &fakeFlagTx{}}
		var bound pgx.Tx
		if err := newTestFlagRepository(queries, txs, &bound).UpsertFlags(t.Context(), flags); err != nil {
			t.Fatalf("UpsertFlags() error = %v", err)
		}
		want := []gen.UpsertFeatureFlagParams{
			{Name: "alpha", Enabled: true, Audience: "all", UserIds: []int64{}},
			{Name: "targeted", Enabled: true, Audience: "user_ids", UserIds: []int64{2, 4}},
		}
		if !reflect.DeepEqual(queries.upserts, want) {
			t.Fatalf("upserts = %+v; want %+v", queries.upserts, want)
		}
		if txs.begins != 1 || bound != txs.tx || txs.tx.commits != 1 || txs.tx.rollbacks != 1 {
			t.Fatalf("begins %d, bound %v, commits %d, rollbacks %d", txs.begins, bound == txs.tx, txs.tx.commits, txs.tx.rollbacks)
		}
	})

	for _, test := range []struct {
		name      string
		list      []domain.FeatureFlag
		queries   *fakeFlagQueries
		txs       *fakeFlagTransactions
		want      string
		begins    int
		commits   int
		rollbacks int
	}{
		{name: "empty", want: "at least one", txs: &fakeFlagTransactions{tx: &fakeFlagTx{}}},
		{name: "invalid", list: []domain.FeatureFlag{{Name: "INVALID", Audience: domain.AudienceAll}}, want: "invalid feature flag", txs: &fakeFlagTransactions{tx: &fakeFlagTx{}}},
		{name: "begin", list: flags, want: "begin feature flag update", txs: &fakeFlagTransactions{beginErr: errors.New("begin failed")}, begins: 1},
		{
			name:      "upsert fails on the second flag",
			list:      flags,
			queries:   &fakeFlagQueries{upsertErr: errors.New("exec failed"), failAt: 2},
			txs:       &fakeFlagTransactions{tx: &fakeFlagTx{}},
			want:      `save feature flag "targeted"`,
			begins:    1,
			rollbacks: 1,
		},
		{
			name:      "commit",
			list:      flags,
			txs:       &fakeFlagTransactions{tx: &fakeFlagTx{commitErr: errors.New("commit failed")}},
			want:      "commit feature flag update",
			begins:    1,
			commits:   1,
			rollbacks: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			queries := test.queries
			if queries == nil {
				queries = &fakeFlagQueries{}
			}
			var bound pgx.Tx
			err := newTestFlagRepository(queries, test.txs, &bound).UpsertFlags(t.Context(), test.list)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("UpsertFlags() error = %v, want %q", err, test.want)
			}
			if test.txs.begins != test.begins {
				t.Fatalf("begins = %d, want %d", test.txs.begins, test.begins)
			}
			if tx := test.txs.tx; tx != nil && (tx.commits != test.commits || tx.rollbacks != test.rollbacks) {
				t.Fatalf("commits = %d, rollbacks = %d; want %d and %d", tx.commits, tx.rollbacks, test.commits, test.rollbacks)
			}
		})
	}
}
