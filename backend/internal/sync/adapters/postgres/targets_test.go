package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/sync/app"
	"github.com/jooservices/xflickr/migrations"
)

func TestRepositoryCreatePersistsSealedEnvelope(t *testing.T) {
	tx := &targetTxFake{rows: []pgx.Row{targetRow{values: []any{int64(42)}}}}
	database := &targetDatabaseFake{tx: tx}
	repository, err := New(database)
	if err != nil {
		t.Fatal(err)
	}
	sealed := targetSealed()
	created, err := repository.Create(t.Context(), targetRecord(), func(id int64) (crypto.Sealed, error) {
		if id != 42 {
			t.Fatalf("sealer id = %d", id)
		}
		return sealed, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 42 || created.UserID != 7 || created.Status != "connected" {
		t.Fatalf("created target = %+v", created)
	}
	if tx.commits != 1 || tx.rollbacks != 0 || len(tx.execArgs) != 7 {
		t.Fatalf("commits=%d rollbacks=%d insert args=%d", tx.commits, tx.rollbacks, len(tx.execArgs))
	}
	if tx.execArgs[0] != int64(42) || tx.execArgs[1] != int64(7) || tx.execArgs[2] != "drive" || tx.execArgs[3] != "Archive" || tx.execArgs[6] != "connected" {
		t.Fatalf("insert args = %#v", tx.execArgs)
	}
	stored, ok := tx.execArgs[4].([]byte)
	if !ok {
		t.Fatalf("sealed insert argument type = %T", tx.execArgs[4])
	}
	var got crypto.Sealed
	if err := got.Scan(stored); err != nil {
		t.Fatal(err)
	}
	if got.KeyID != sealed.KeyID || !bytes.Equal(got.Ciphertext, sealed.Ciphertext) {
		t.Fatalf("stored sealed value = %+v", got)
	}
}

func TestRepositoryCreateRollsBackFailures(t *testing.T) {
	sealErr := errors.New("seal failed")
	tx := &targetTxFake{rows: []pgx.Row{targetRow{values: []any{int64(43)}}}}
	repository, err := New(&targetDatabaseFake{tx: tx})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(t.Context(), targetRecord(), func(int64) (crypto.Sealed, error) {
		return crypto.Sealed{}, sealErr
	}); !errors.Is(err, sealErr) {
		t.Fatalf("seal error = %v", err)
	}
	if tx.execCalls != 0 || tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatalf("exec=%d rollback=%d commit=%d", tx.execCalls, tx.rollbacks, tx.commits)
	}

	insertErr := errors.New("insert failed")
	tx = &targetTxFake{rows: []pgx.Row{targetRow{values: []any{int64(44)}}}, execErr: insertErr}
	repository, _ = New(&targetDatabaseFake{tx: tx})
	if _, err := repository.Create(t.Context(), targetRecord(), func(int64) (crypto.Sealed, error) {
		return targetSealed(), nil
	}); !errors.Is(err, insertErr) {
		t.Fatalf("insert error = %v", err)
	}
	if tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatalf("insert rollback=%d commit=%d", tx.rollbacks, tx.commits)
	}
}

func TestRepositoryCreateReportsAllocationAndEnvelopeErrors(t *testing.T) {
	allocationErr := errors.New("sequence unavailable")
	tx := &targetTxFake{rows: []pgx.Row{targetRow{err: allocationErr}}}
	repository, _ := New(&targetDatabaseFake{tx: tx})
	if _, err := repository.Create(t.Context(), targetRecord(), func(int64) (crypto.Sealed, error) {
		return targetSealed(), nil
	}); !errors.Is(err, allocationErr) || !strings.Contains(err.Error(), "allocate target id") {
		t.Fatalf("allocation error = %v", err)
	}
	if tx.execCalls != 0 || tx.rollbacks != 1 {
		t.Fatalf("exec=%d rollback=%d", tx.execCalls, tx.rollbacks)
	}

	tx = &targetTxFake{rows: []pgx.Row{targetRow{values: []any{int64(45)}}}}
	repository, _ = New(&targetDatabaseFake{tx: tx})
	if _, err := repository.Create(t.Context(), targetRecord(), func(int64) (crypto.Sealed, error) {
		return crypto.Sealed{}, nil
	}); err == nil || !strings.Contains(err.Error(), "target token") {
		t.Fatalf("invalid envelope error = %v", err)
	}
	if tx.execCalls != 0 || tx.rollbacks != 1 {
		t.Fatalf("invalid envelope exec=%d rollback=%d", tx.execCalls, tx.rollbacks)
	}
}

func TestRepositoryCreateRejectsMissingSealerAndReturnsCommitFailure(t *testing.T) {
	repository, _ := New(&targetDatabaseFake{tx: &targetTxFake{rows: []pgx.Row{targetRow{values: []any{int64(1)}}}}})
	if _, err := repository.Create(t.Context(), targetRecord(), nil); err == nil || !strings.Contains(err.Error(), "sealer") {
		t.Fatalf("missing sealer error = %v", err)
	}

	commitErr := errors.New("commit failed")
	tx := &targetTxFake{rows: []pgx.Row{targetRow{values: []any{int64(2)}}}, commitErr: commitErr}
	repository, _ = New(&targetDatabaseFake{tx: tx})
	if _, err := repository.Create(t.Context(), targetRecord(), func(int64) (crypto.Sealed, error) {
		return targetSealed(), nil
	}); !errors.Is(err, commitErr) {
		t.Fatalf("commit error = %v", err)
	}
	if tx.commits != 1 {
		t.Fatalf("commit calls = %d", tx.commits)
	}
}

func TestRepositoryGetScopesByOwnerAndMapsMissingRows(t *testing.T) {
	values := []any{int64(42), int64(7), "drive", "Archive", []byte(`{"folder":"archive"}`), "connected"}
	tx := &targetTxFake{rows: []pgx.Row{targetRow{values: values}}}
	repository, err := New(&targetDatabaseFake{tx: tx})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(t.Context(), 7, 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.UserID != 7 || got.Provider != "drive" || got.Label != "Archive" || got.Status != "connected" || string(got.Config) != `{"folder":"archive"}` {
		t.Fatalf("target = %+v", got)
	}
	if len(tx.rowArgs) != 2 || tx.rowArgs[0] != int64(42) || tx.rowArgs[1] != int64(7) {
		t.Fatalf("owner-scoped query args = %#v", tx.rowArgs)
	}

	tx = &targetTxFake{rows: []pgx.Row{targetRow{err: pgx.ErrNoRows}}}
	repository, _ = New(&targetDatabaseFake{tx: tx})
	if _, err := repository.Get(t.Context(), 8, 42); !errors.Is(err, app.ErrTargetNotFound) {
		t.Fatalf("missing row error = %v", err)
	}

	scanErr := errors.New("read failed")
	tx = &targetTxFake{rows: []pgx.Row{targetRow{err: scanErr}}}
	repository, _ = New(&targetDatabaseFake{tx: tx})
	if _, err := repository.Get(t.Context(), 7, 42); !errors.Is(err, scanErr) || !strings.Contains(err.Error(), "get target") {
		t.Fatalf("read error = %v", err)
	}
}

func TestRepositoryListScopesOwnerAndCursor(t *testing.T) {
	rows := &targetRowsFake{values: [][]any{
		{int64(43), int64(7), "drive", "Archive", []byte(`{"folder":"archive"}`), "connected"},
		{int64(44), int64(7), "dropbox", "Photos", []byte(`{}`), "reconnect_required"},
	}}
	tx := &targetTxFake{queryRows: rows}
	repository, err := New(&targetDatabaseFake{tx: tx})
	if err != nil {
		t.Fatal(err)
	}

	got, err := repository.List(t.Context(), 7, 42, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 43 || got[1].ID != 44 || got[1].Status != "reconnect_required" || string(got[0].Config) != `{"folder":"archive"}` {
		t.Fatalf("listed targets = %+v", got)
	}
	if len(tx.queryArgs) != 3 || tx.queryArgs[0] != int64(7) || tx.queryArgs[1] != int64(42) || tx.queryArgs[2] != 2 {
		t.Fatalf("list query args = %#v", tx.queryArgs)
	}
	if !rows.closed || tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("closed=%t commits=%d rollbacks=%d", rows.closed, tx.commits, tx.rollbacks)
	}
}

func TestRepositoryListHandlesQueryAndRowFailures(t *testing.T) {
	queryErr := errors.New("list query failed")
	tx := &targetTxFake{queryErr: queryErr}
	repository, _ := New(&targetDatabaseFake{tx: tx})
	if _, err := repository.List(t.Context(), 7, 0, 5); !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "list targets") {
		t.Fatalf("query error = %v", err)
	}
	if tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatalf("query rollback=%d commit=%d", tx.rollbacks, tx.commits)
	}

	scanErr := errors.New("target row failed")
	rows := &targetRowsFake{values: [][]any{{int64(43), int64(7), "drive", "Archive", []byte(`{}`), "connected"}}, scanErr: scanErr}
	tx = &targetTxFake{queryRows: rows}
	repository, _ = New(&targetDatabaseFake{tx: tx})
	if _, err := repository.List(t.Context(), 7, 0, 5); !errors.Is(err, scanErr) || !strings.Contains(err.Error(), "scan target") {
		t.Fatalf("scan error = %v", err)
	}
	if !rows.closed || tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatalf("scan closed=%t rollback=%d commit=%d", rows.closed, tx.rollbacks, tx.commits)
	}

	iterationErr := errors.New("target iteration failed")
	rows = &targetRowsFake{rowErr: iterationErr}
	tx = &targetTxFake{queryRows: rows}
	repository, _ = New(&targetDatabaseFake{tx: tx})
	if _, err := repository.List(t.Context(), 7, 0, 5); !errors.Is(err, iterationErr) || !strings.Contains(err.Error(), "list targets") {
		t.Fatalf("iteration error = %v", err)
	}
	if !rows.closed || tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatalf("iteration closed=%t rollback=%d commit=%d", rows.closed, tx.rollbacks, tx.commits)
	}
}

func TestRepositoryRejectsMissingDatabaseAndContext(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("constructor accepted nil database")
	}
	for _, repository := range []*Repository{nil, {}} {
		if _, err := repository.Get(t.Context(), 7, 42); err == nil {
			t.Fatal("Get accepted missing database")
		}
		if _, err := repository.List(t.Context(), 7, 0, 1); err == nil {
			t.Fatal("List accepted missing database")
		}
	}
	repository, _ := New(&targetDatabaseFake{tx: &targetTxFake{}})
	if _, err := repository.Get(nil, 7, 42); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := repository.List(nil, 7, 0, 1); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil list context error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.Get(canceled, 7, 42); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	if _, err := repository.List(canceled, 7, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list context error = %v", err)
	}
}

func TestTargetsPostgresRoundTrip(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cfg := targetDatabaseConfig(t)
	pool := dbtest.Open(t, cfg)
	userID := targetTestUser(t, ctx, pool)
	t.Cleanup(func() { deleteTargetTestUser(t, pool, userID) })
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ring := targetTestKeyring(t)
	service := app.Targets{Repository: repository, Sealer: ring}
	plaintext := []byte("target-token-roundtrip")
	created, err := service.Create(ctx, app.Target{UserID: userID, Provider: "drive", Label: "Archive", Config: json.RawMessage(`{"folder":"archive"}`)}, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(ctx, app.Target{UserID: userID, Provider: "dropbox", Label: "Photos", Config: json.RawMessage(`{}`)}, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	otherUserID := targetTestUser(t, ctx, pool)
	t.Cleanup(func() { deleteTargetTestUser(t, pool, otherUserID) })
	if _, err := service.Create(ctx, app.Target{UserID: otherUserID, Provider: "drive", Label: "Other", Config: json.RawMessage(`{}`)}, plaintext); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT token_sealed FROM sync_targets WHERE id = $1 AND user_id = $2`, created.ID, userID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, plaintext) {
		t.Fatal("token column contains plaintext")
	}
	var sealed crypto.Sealed
	if err := sealed.Scan(stored); err != nil {
		t.Fatal(err)
	}
	opened, err := ring.Open(sealed, app.TargetTokenAAD(created.ID))
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("open target token: matches=%t, error=%v", bytes.Equal(opened, plaintext), err)
	}
	if _, err := ring.Open(sealed, app.TargetTokenAAD(created.ID+1)); !errors.Is(err, crypto.ErrAuthentication) {
		t.Fatalf("wrong AAD error = %v", err)
	}
	loaded, err := service.Get(ctx, userID, created.ID)
	if err != nil || loaded.ID != created.ID || loaded.Provider != "drive" || loaded.Label != "Archive" || !jsonEqual(t, loaded.Config, []byte(`{"folder":"archive"}`)) {
		t.Fatalf("loaded target = %+v, error = %v", loaded, err)
	}
	if _, err := service.Get(ctx, userID+1, created.ID); !errors.Is(err, app.ErrTargetNotFound) {
		t.Fatalf("other owner error = %v", err)
	}
	if _, err := service.Get(ctx, userID, second.ID+1); !errors.Is(err, app.ErrTargetNotFound) {
		t.Fatalf("missing target error = %v", err)
	}
	firstPage, err := service.List(ctx, userID, 0, 1)
	if err != nil || len(firstPage) != 1 || firstPage[0].ID != created.ID {
		t.Fatalf("first target page = %+v, error = %v", firstPage, err)
	}
	secondPage, err := service.List(ctx, userID, firstPage[0].ID, 1)
	if err != nil || len(secondPage) != 1 || secondPage[0].ID != second.ID {
		t.Fatalf("second target page = %+v, error = %v", secondPage, err)
	}
	otherTargets, err := service.List(ctx, otherUserID, 0, 5)
	if err != nil || len(otherTargets) != 1 || otherTargets[0].UserID != otherUserID {
		t.Fatalf("other user's targets = %+v, error = %v", otherTargets, err)
	}
	emptyPage, err := service.List(ctx, userID, secondPage[0].ID, 5)
	if err != nil || len(emptyPage) != 0 {
		t.Fatalf("empty target page = %+v, error = %v", emptyPage, err)
	}

	failure := targetSealerFailure{err: errors.New("seal failed")}
	_, err = (app.Targets{Repository: repository, Sealer: failure}).Create(ctx, app.Target{UserID: userID, Provider: "drive", Label: "Rejected", Config: json.RawMessage(`{}`)}, plaintext)
	if err == nil {
		t.Fatal("Create accepted a failed seal")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sync_targets WHERE user_id = $1 AND label = 'Rejected')`, userID).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("target row exists after seal failure")
	}

	rollbackTargetMigrations(t, ctx, cfg)
	var targetSurvivedRollback bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sync_targets WHERE id = $1 AND user_id = $2)`, created.ID, userID).Scan(&targetSurvivedRollback); err != nil {
		t.Fatalf("check worktree target after isolated rollback: %v", err)
	}
	if !targetSurvivedRollback {
		t.Fatal("worktree target was removed by the migration rollback check")
	}
}

func TestTargetQueriesUseIndexes(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, targetDatabaseConfig(t))
	userID := targetTestUser(t, ctx, pool)
	t.Cleanup(func() { deleteTargetTestUser(t, pool, userID) })
	fixtureUserID := targetTestUser(t, ctx, pool)
	t.Cleanup(func() { deleteTargetTestUser(t, pool, fixtureUserID) })
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repository.Create(ctx, app.Target{UserID: userID, Provider: "drive", Label: "Plan target", Config: json.RawMessage(`{}`), Status: "connected"}, func(int64) (crypto.Sealed, error) {
		return targetSealed(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixtureRows = 400000
	if _, err := pool.Exec(ctx, `INSERT INTO sync_targets (user_id, provider, label, token_sealed, config, status)
SELECT $1, 'drive', 'plan-fixture-' || item::text, $2, '{}'::jsonb, 'connected'
FROM generate_series(1, $3::integer) AS item`, fixtureUserID, []byte("fixture-envelope"), fixtureRows); err != nil {
		t.Fatalf("insert plan fixtures: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE sync_targets`); err != nil {
		t.Fatalf("analyze target fixtures: %v", err)
	}
	var encoded []byte
	if err := pool.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+getSQL, created.ID, userID).Scan(&encoded); err != nil {
		t.Fatalf("explain owner read: %v", err)
	}
	var plans []struct {
		Plan targetPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal(encoded, &plans); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plan roots = %d", len(plans))
	}
	t.Logf("owner-scoped target query plan on %d rows: %s", fixtureRows+1, encoded)
	indexes, scans := targetPlanEvidence(plans[0].Plan)
	if !indexes["sync_targets_pkey"] && !indexes["sync_targets_user_id_id_idx"] {
		t.Fatalf("plan does not use a target lookup index: %s", encoded)
	}
	if scans {
		t.Fatalf("plan contains a sync_targets Seq Scan: %s", encoded)
	}
	encoded = nil
	if err := pool.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+listSQL, userID, int64(0), 5).Scan(&encoded); err != nil {
		t.Fatalf("explain owner-scoped target page: %v", err)
	}
	if err := json.Unmarshal(encoded, &plans); err != nil {
		t.Fatalf("decode list plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("list plan roots = %d", len(plans))
	}
	t.Logf("owner-scoped target page plan on %d rows: %s", fixtureRows+1, encoded)
	indexes, scans = targetPlanEvidence(plans[0].Plan)
	if !indexes["sync_targets_user_id_id_idx"] {
		t.Fatalf("plan does not use sync_targets_user_id_id_idx: %s", encoded)
	}
	if scans {
		t.Fatalf("plan contains a sync_targets Seq Scan: %s", encoded)
	}
}

func targetRecord() app.Target {
	return app.Target{UserID: 7, Provider: "drive", Label: "Archive", Config: json.RawMessage(`{"folder":"archive"}`), Status: "connected"}
}

func targetSealed() crypto.Sealed {
	return crypto.Sealed{
		KeyID:      "test",
		Nonce:      bytes.Repeat([]byte{1}, 12),
		Ciphertext: bytes.Repeat([]byte{2}, 16),
		WrappedDEK: bytes.Repeat([]byte{3}, 60),
	}
}

type targetDatabaseFake struct {
	tx        *targetTxFake
	beginErr  error
	beginCall int
}

func (database *targetDatabaseFake) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	database.beginCall++
	if database.beginErr != nil {
		return nil, database.beginErr
	}
	return database.tx, nil
}

type targetTxFake struct {
	pgx.Tx
	rows       []pgx.Row
	queryRows  pgx.Rows
	queryErr   error
	rowArgs    []any
	queryArgs  []any
	execArgs   []any
	queryCalls int
	execCalls  int
	execErr    error
	commitErr  error
	commits    int
	rollbacks  int
}

func (tx *targetTxFake) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	tx.queryCalls++
	tx.queryArgs = append([]any(nil), args...)
	return tx.queryRows, tx.queryErr
}

func (tx *targetTxFake) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	tx.rowArgs = append([]any(nil), args...)
	if len(tx.rows) == 0 {
		return targetRow{err: errors.New("unexpected query row")}
	}
	row := tx.rows[0]
	tx.rows = tx.rows[1:]
	return row
}

func (tx *targetTxFake) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	tx.execArgs = append([]any(nil), args...)
	return pgconn.NewCommandTag("INSERT 0 1"), tx.execErr
}

func (tx *targetTxFake) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

func (tx *targetTxFake) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type targetRow struct {
	values []any
	err    error
}

type targetRowsFake struct {
	pgx.Rows
	values  [][]any
	index   int
	scanErr error
	rowErr  error
	closed  bool
}

func (rows *targetRowsFake) Next() bool {
	if rows.index >= len(rows.values) {
		return false
	}
	rows.index++
	return true
}

func (rows *targetRowsFake) Scan(destinations ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	return (targetRow{values: rows.values[rows.index-1]}).Scan(destinations...)
}

func (rows *targetRowsFake) Err() error { return rows.rowErr }

func (rows *targetRowsFake) Close() { rows.closed = true }

func (row targetRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return fmt.Errorf("scan destinations = %d, values = %d", len(destinations), len(row.values))
	}
	for index, destination := range destinations {
		switch target := destination.(type) {
		case *int64:
			*target = row.values[index].(int64)
		case *string:
			*target = row.values[index].(string)
		case *[]byte:
			*target = append((*target)[:0], row.values[index].([]byte)...)
		case *json.RawMessage:
			*target = append((*target)[:0], row.values[index].([]byte)...)
		default:
			return fmt.Errorf("unsupported destination %T", destination)
		}
	}
	return nil
}

type targetSealerFailure struct{ err error }

func (failure targetSealerFailure) Seal([]byte, []byte) (crypto.Sealed, error) {
	return crypto.Sealed{}, failure.err
}

type targetPlanNode struct {
	NodeType string           `json:"Node Type"`
	Relation string           `json:"Relation Name"`
	Index    string           `json:"Index Name"`
	Subplans []targetPlanNode `json:"Plans"`
}

func targetPlanEvidence(node targetPlanNode) (map[string]bool, bool) {
	indexes := make(map[string]bool)
	seqScan := node.NodeType == "Seq Scan" && node.Relation == "sync_targets"
	if node.Index != "" {
		indexes[node.Index] = true
	}
	for _, child := range node.Subplans {
		childIndexes, childSeqScan := targetPlanEvidence(child)
		for name := range childIndexes {
			indexes[name] = true
		}
		seqScan = seqScan || childSeqScan
	}
	return indexes, seqScan
}

func targetDatabaseConfig(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate target integration test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func targetTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var userID int64
	email := fmt.Sprintf("jooxf146-%d@example.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale) VALUES ($1, $2, 'user', 'en') RETURNING id`, email, "integration-hash").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return userID
}

func deleteTargetTestUser(t *testing.T, pool *pgxpool.Pool, userID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Errorf("delete target integration user: %v", err)
	}
}

func targetTestKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	ring, err := crypto.NewKeyring(map[string][]byte{"target-test": bytes.Repeat([]byte{7}, 32)}, "target-test")
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func rollbackTargetMigrations(t *testing.T, ctx context.Context, cfg config.DBConfig) {
	t.Helper()
	isolatedCfg := dbtest.EmptyDatabase(t, cfg)
	if isolatedCfg.Name == cfg.Name {
		t.Fatal("migration rollback database reused the worktree database")
	}
	isolatedPool, err := db.NewPool(ctx, isolatedCfg)
	if err != nil {
		t.Fatalf("open isolated migration rollback database: %v", err)
	}
	defer isolatedPool.Close()
	var currentDatabase string
	if err := isolatedPool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDatabase); err != nil {
		t.Fatalf("read isolated migration rollback database: %v", err)
	}
	if currentDatabase != isolatedCfg.Name {
		t.Fatalf("migration rollback connected to %q, want isolated database %q", currentDatabase, isolatedCfg.Name)
	}
	if _, err := isolatedPool.Exec(ctx, `CREATE TABLE users (id BIGINT PRIMARY KEY)`); err != nil {
		t.Fatalf("prepare isolated migration rollback user table: %v", err)
	}
	const targetMigration = "20261111131000_sync_targets.sql"
	targetSQL, err := fs.ReadFile(migrations.Files, targetMigration)
	if err != nil {
		t.Fatalf("read sync target table migration: %v", err)
	}
	const indexMigration = "20261112100000_sync_targets_owner_cursor_index.sql"
	indexSQL, err := fs.ReadFile(migrations.Files, indexMigration)
	if err != nil {
		t.Fatalf("read sync target index migration: %v", err)
	}
	files := fstest.MapFS{
		targetMigration: &fstest.MapFile{Data: targetSQL},
		indexMigration:  &fstest.MapFile{Data: indexSQL},
	}
	if err := db.MigrateFiles(ctx, isolatedCfg, db.MigrateUp, nil, files); err != nil {
		t.Fatalf("apply sync target migrations to isolated database: %v", err)
	}
	if err := db.MigrateFiles(ctx, isolatedCfg, db.MigrateDown, nil, files); err != nil {
		t.Fatalf("roll back sync target index migration: %v", err)
	}
	var index *string
	if err := isolatedPool.QueryRow(ctx, `SELECT to_regclass('public.sync_targets_user_id_id_idx')::text`).Scan(&index); err != nil {
		t.Fatalf("read rolled back target index: %v", err)
	}
	if index != nil {
		t.Fatalf("sync_targets_user_id_id_idx still exists after rollback: %s", *index)
	}
	if err := db.MigrateFiles(ctx, isolatedCfg, db.MigrateDown, nil, files); err != nil {
		t.Fatalf("roll back sync target table migration: %v", err)
	}
	var table *string
	if err := isolatedPool.QueryRow(ctx, `SELECT to_regclass('public.sync_targets')::text`).Scan(&table); err != nil {
		t.Fatalf("read rolled back table: %v", err)
	}
	if table != nil {
		t.Fatalf("sync_targets still exists after rollback: %s", *table)
	}
}

// jsonEqual compares two JSON documents structurally: PostgreSQL stores jsonb in a normalised form,
// so the stored text can differ from the input in whitespace and key order.
func jsonEqual(t *testing.T, got, want []byte) bool {
	t.Helper()
	var left, right any
	if err := json.Unmarshal(got, &left); err != nil {
		t.Fatalf("decode stored JSON %q: %v", got, err)
	}
	if err := json.Unmarshal(want, &right); err != nil {
		t.Fatalf("decode expected JSON %q: %v", want, err)
	}
	return reflect.DeepEqual(left, right)
}
