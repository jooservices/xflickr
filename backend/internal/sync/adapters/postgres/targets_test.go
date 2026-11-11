package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/sync/app"
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

func TestRepositoryRejectsMissingDatabaseAndContext(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("constructor accepted nil database")
	}
	for _, repository := range []*Repository{nil, {}} {
		if _, err := repository.Get(t.Context(), 7, 42); err == nil {
			t.Fatal("Get accepted missing database")
		}
	}
	repository, _ := New(&targetDatabaseFake{tx: &targetTxFake{}})
	if _, err := repository.Get(nil, 7, 42); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.Get(canceled, 7, 42); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
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
	if err != nil || loaded.ID != created.ID || loaded.Provider != "drive" || loaded.Label != "Archive" || string(loaded.Config) != `{"folder":"archive"}` {
		t.Fatalf("loaded target = %+v, error = %v", loaded, err)
	}
	if _, err := service.Get(ctx, userID+1, created.ID); !errors.Is(err, app.ErrTargetNotFound) {
		t.Fatalf("other owner error = %v", err)
	}
	if _, err := service.Get(ctx, userID, created.ID+1); !errors.Is(err, app.ErrTargetNotFound) {
		t.Fatalf("missing target error = %v", err)
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

	rollbackTargetMigration(t, ctx, cfg, pool)
}

func TestTargetsGetPlanUsesPrimaryKey(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, targetDatabaseConfig(t))
	userID := targetTestUser(t, ctx, pool)
	t.Cleanup(func() { deleteTargetTestUser(t, pool, userID) })
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
FROM generate_series(1, $3::integer) AS item`, userID, []byte("fixture-envelope"), fixtureRows); err != nil {
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
	if !indexes["sync_targets_pkey"] {
		t.Fatalf("plan does not use sync_targets_pkey: %s", encoded)
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
	rows      []pgx.Row
	rowArgs   []any
	execArgs  []any
	execCalls int
	execErr   error
	commitErr error
	commits   int
	rollbacks int
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

func rollbackTargetMigration(t *testing.T, ctx context.Context, cfg config.DBConfig, pool *pgxpool.Pool) {
	t.Helper()
	restored := false
	t.Cleanup(func() {
		if restored {
			return
		}
		if err := db.Migrate(context.Background(), cfg, db.MigrateUp, nil); err != nil {
			t.Errorf("restore sync target migration: %v", err)
		}
	})
	if err := db.Migrate(ctx, cfg, db.MigrateDown, nil); err != nil {
		t.Fatalf("roll back sync target migration: %v", err)
	}
	var table *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.sync_targets')::text`).Scan(&table); err != nil {
		t.Fatalf("read rolled back table: %v", err)
	}
	if table != nil {
		t.Fatalf("sync_targets still exists after rollback: %s", *table)
	}
	if err := db.Migrate(ctx, cfg, db.MigrateUp, nil); err != nil {
		t.Fatalf("reapply sync target migration: %v", err)
	}
	restored = true
}
