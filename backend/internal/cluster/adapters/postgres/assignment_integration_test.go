package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/cluster/adapters/postgres"
	"github.com/jooservices/xflickr/internal/cluster/app"
	"github.com/jooservices/xflickr/internal/cluster/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestAssignmentInitialIntegration(t *testing.T) {
	pool, ctx := openAssignmentDatabase(t)
	fixture := newAssignmentFixture(t, ctx, pool)
	now := assignmentTime()
	keyID := fixture.addKey(t, ctx, "valid")
	node1 := fixture.addNode(t, ctx, "node-1", "online", []string{"crawl"}, &now, nil)
	node2 := fixture.addNode(t, ctx, "node-2", "online", []string{"crawl"}, &now, nil)

	service := app.Assignments{Repository: postgres.New(pool), Now: func() time.Time { return now }}
	first, err := service.AssignInitial(ctx, keyID, node1)
	if err != nil {
		t.Fatalf("assign key: %v", err)
	}
	if first.KeyID != keyID || first.Node != node1 || !first.Since.Equal(now) || first.Reason != "initial" {
		t.Fatalf("assignment = %+v", first)
	}

	var stored app.Assignment
	if err := pool.QueryRow(ctx, `
SELECT key_id, node, since, reason
FROM key_assignments WHERE key_id = $1`, keyID).Scan(&stored.KeyID, &stored.Node, &stored.Since, &stored.Reason); err != nil {
		t.Fatalf("read stored assignment: %v", err)
	}
	if stored != first {
		t.Fatalf("stored assignment = %+v, want %+v", stored, first)
	}

	later := now.Add(time.Minute)
	retryService := app.Assignments{Repository: postgres.New(pool), Now: func() time.Time { return later }}
	retried, err := retryService.AssignInitial(ctx, keyID, node2)
	if err != nil {
		t.Fatalf("repeat assignment: %v", err)
	}
	if retried != first {
		t.Fatalf("repeat assignment = %+v, want original %+v", retried, first)
	}
	queue, err := service.QueueFor(ctx, keyID)
	if err != nil {
		t.Fatalf("lookup key queue: %v", err)
	}
	if queue != "crawl@"+node1 {
		t.Fatalf("queue = %q, want %q", queue, "crawl@"+node1)
	}

	boundaryKey := fixture.addKey(t, ctx, "valid")
	boundaryHeartbeat := now.Add(-domain.WarningAfter)
	boundaryNode := fixture.addNode(t, ctx, "warning-boundary", "online", []string{"crawl"}, &boundaryHeartbeat, nil)
	boundary, err := service.AssignInitial(ctx, boundaryKey, boundaryNode)
	if err != nil {
		t.Fatalf("assign at warning threshold: %v", err)
	}
	if boundary.Node != boundaryNode {
		t.Fatalf("boundary assignment node = %q, want %q", boundary.Node, boundaryNode)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, keyID); err != nil {
		t.Fatalf("delete assigned key: %v", err)
	}
	var assignmentExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM key_assignments WHERE key_id = $1)`, keyID).Scan(&assignmentExists); err != nil {
		t.Fatalf("check deleted key assignment: %v", err)
	}
	if assignmentExists {
		t.Fatal("assignment was not deleted with its API key")
	}
}

func TestAssignmentRejectsUnavailableIntegration(t *testing.T) {
	pool, ctx := openAssignmentDatabase(t)
	fixture := newAssignmentFixture(t, ctx, pool)
	now := assignmentTime()
	validKey := fixture.addKey(t, ctx, "valid")
	missingKey := fixture.addKey(t, ctx, "valid")
	if _, err := pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, missingKey); err != nil {
		t.Fatalf("delete key for missing-key case: %v", err)
	}
	invalidKey := fixture.addKey(t, ctx, "invalid")
	validNode := fixture.addNode(t, ctx, "valid", "online", []string{"crawl"}, &now, nil)
	offlineNode := fixture.addNode(t, ctx, "offline", "offline", []string{"crawl"}, &now, nil)
	coolingUntil := now.Add(time.Minute)
	coolingNode := fixture.addNode(t, ctx, "cooling", "online", []string{"crawl"}, &now, &coolingUntil)
	otherRoleNode := fixture.addNode(t, ctx, "other-role", "online", []string{"media"}, &now, nil)
	staleHeartbeat := now.Add(-domain.WarningAfter - time.Second)
	staleNode := fixture.addNode(t, ctx, "stale", "online", []string{"crawl"}, &staleHeartbeat, nil)

	service := app.Assignments{Repository: postgres.New(pool), Now: func() time.Time { return now }}
	for _, test := range []struct {
		name  string
		keyID int64
		node  string
	}{
		{name: "missing key", keyID: missingKey, node: validNode},
		{name: "invalid key", keyID: invalidKey, node: validNode},
		{name: "missing node", keyID: validKey, node: fixture.suffix + "-missing"},
		{name: "offline node", keyID: validKey, node: offlineNode},
		{name: "cooling node", keyID: validKey, node: coolingNode},
		{name: "node without crawl role", keyID: validKey, node: otherRoleNode},
		{name: "old heartbeat", keyID: validKey, node: staleNode},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.AssignInitial(ctx, test.keyID, test.node); !errors.Is(err, app.ErrAssignmentUnavailable) {
				t.Fatalf("AssignInitial() error = %v", err)
			}
			var assignmentExists bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM key_assignments WHERE key_id = $1)`, test.keyID).Scan(&assignmentExists); err != nil {
				t.Fatalf("check unavailable assignment: %v", err)
			}
			if assignmentExists {
				t.Fatalf("unavailable key %d received an assignment", test.keyID)
			}
		})
	}
	if _, err := service.QueueFor(ctx, validKey); !errors.Is(err, app.ErrAssignmentUnavailable) {
		t.Fatalf("QueueFor() error for unassigned key = %v", err)
	}
}

func TestConcurrentInitialAssignmentIntegration(t *testing.T) {
	pool, ctx := openAssignmentDatabase(t)
	fixture := newAssignmentFixture(t, ctx, pool)
	now := assignmentTime()
	keyID := fixture.addKey(t, ctx, "valid")
	node1 := fixture.addNode(t, ctx, "node-1", "online", []string{"crawl"}, &now, nil)
	node2 := fixture.addNode(t, ctx, "node-2", "online", []string{"crawl"}, &now, nil)
	repository := postgres.New(pool)

	type result struct {
		assignment app.Assignment
		err        error
	}
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for _, node := range []string{node1, node2} {
		wait.Add(1)
		go func(node string) {
			defer wait.Done()
			assignment, err := (app.Assignments{Repository: repository, Now: func() time.Time { return now }}).AssignInitial(ctx, keyID, node)
			results <- result{assignment: assignment, err: err}
		}(node)
	}
	wait.Wait()
	close(results)

	var first app.Assignment
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent assignment: %v", result.err)
		}
		if first.KeyID == 0 {
			first = result.assignment
			continue
		}
		if result.assignment != first {
			t.Fatalf("concurrent assignments differ: %+v and %+v", first, result.assignment)
		}
	}
	if first.KeyID != keyID || (first.Node != node1 && first.Node != node2) {
		t.Fatalf("concurrent result = %+v", first)
	}

	var assignmentExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM key_assignments WHERE key_id = $1)`, keyID).Scan(&assignmentExists); err != nil {
		t.Fatalf("check key assignment: %v", err)
	}
	if !assignmentExists {
		t.Fatal("concurrent assignment did not persist a row")
	}
}

func TestAssignmentDatabaseErrorsAndCancellationIntegration(t *testing.T) {
	pool, _ := openAssignmentDatabase(t)
	repository := postgres.New(pool)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := repository.AssignInitial(ctx, 1, "node-1", assignmentTime()); !errors.Is(err, context.Canceled) {
		t.Fatalf("AssignInitial() error = %v, want context cancellation", err)
	}
	if _, err := repository.GetAssignment(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetAssignment() error = %v, want context cancellation", err)
	}
}

func TestAssignmentLookupPlanIntegration(t *testing.T) {
	pool, ctx := openAssignmentDatabase(t)
	fixture := newAssignmentFixture(t, ctx, pool)
	now := assignmentTime()
	node := fixture.addNode(t, ctx, "plan-node", "online", []string{"crawl"}, &now, nil)
	const fixtureSize = 400_000

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin plan fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback plan fixture transaction: %v", err)
		}
	})
	var keyID int64
	err = tx.QueryRow(ctx, `
WITH inserted_keys AS (
    INSERT INTO api_keys
        (user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst)
    SELECT $1, 'plan-fixture', $2 || '-' || n::text, '{}'::jsonb, 100, 10, 20
    FROM generate_series(1, $4::integer) AS generated(n)
    RETURNING id
), inserted_assignments AS (
    INSERT INTO key_assignments (key_id, node, since, reason)
    SELECT id, $3, $5, 'initial' FROM inserted_keys
    RETURNING key_id
)
SELECT min(key_id) FROM inserted_assignments`, fixture.userID, fixture.suffix+"-plan", node, fixtureSize, now).Scan(&keyID)
	if err != nil {
		t.Fatalf("seed assignment lookup plan fixture: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit plan fixture: %v", err)
	}

	if _, err := pool.Exec(ctx, `ANALYZE key_assignments`); err != nil {
		t.Fatalf("analyze assignment fixture: %v", err)
	}
	var rawPlan []byte
	if err := pool.QueryRow(ctx, `
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT key_id, node, since, reason FROM key_assignments WHERE key_id = $1`, keyID).Scan(&rawPlan); err != nil {
		t.Fatalf("explain assignment lookup: %v", err)
	}
	var plan []assignmentPlanEnvelope
	if err := json.Unmarshal(rawPlan, &plan); err != nil {
		t.Fatalf("decode assignment lookup plan: %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("explain plan entries = %d, want 1", len(plan))
	}
	usesPrimaryKey := false
	var inspectPlan func(assignmentPlanNode)
	inspectPlan = func(node assignmentPlanNode) {
		if node.NodeType == "Seq Scan" && node.RelationName == "key_assignments" {
			t.Errorf("assignment lookup uses a Seq Scan on key_assignments")
		}
		if node.IndexName == "key_assignments_pkey" {
			usesPrimaryKey = true
		}
		for _, child := range node.Plans {
			inspectPlan(child)
		}
	}
	inspectPlan(plan[0].Plan)
	if !usesPrimaryKey {
		t.Fatalf("assignment lookup plan does not use key_assignments_pkey: %s", rawPlan)
	}
}

type assignmentPlanEnvelope struct {
	Plan assignmentPlanNode `json:"Plan"`
}

type assignmentPlanNode struct {
	NodeType     string               `json:"Node Type"`
	RelationName string               `json:"Relation Name"`
	IndexName    string               `json:"Index Name"`
	Plans        []assignmentPlanNode `json:"Plans"`
}

type assignmentFixture struct {
	pool   *pgxpool.Pool
	userID int64
	suffix string
	keyNum int
	nodes  []string
}

func openAssignmentDatabase(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree database")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test source path")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)
	return pool, ctx
}

func newAssignmentFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *assignmentFixture {
	t.Helper()
	suffix := fmt.Sprintf("assignment-%d", time.Now().UnixNano())
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role) VALUES ($1, 'unused', 'user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("create assignment test user: %v", err)
	}
	fixture := &assignmentFixture{pool: pool, userID: userID, suffix: suffix}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, fixture.userID); err != nil {
			t.Errorf("delete assignment test user: %v", err)
		}
		for _, node := range fixture.nodes {
			if _, err := pool.Exec(cleanupCtx, `DELETE FROM nodes WHERE name = $1`, node); err != nil {
				t.Errorf("delete assignment test node: %v", err)
			}
		}
	})
	return fixture
}

func (fixture *assignmentFixture) addKey(t *testing.T, ctx context.Context, status string) int64 {
	t.Helper()
	fixture.keyNum++
	var keyID int64
	err := fixture.pool.QueryRow(ctx, `
INSERT INTO api_keys
    (user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst, status)
VALUES ($1, 'assignment-test', $2, '{}'::jsonb, 100, 10, 20, $3)
RETURNING id`, fixture.userID, fmt.Sprintf("%s-key-%d", fixture.suffix, fixture.keyNum), status).Scan(&keyID)
	if err != nil {
		t.Fatalf("create assignment test key: %v", err)
	}
	return keyID
}

func (fixture *assignmentFixture) addNode(t *testing.T, ctx context.Context, suffix, state string, roles []string, heartbeat, cooling *time.Time) string {
	t.Helper()
	name := fixture.suffix + "-" + suffix
	tokenHash := make([]byte, 32)
	if _, err := rand.Read(tokenHash); err != nil {
		t.Fatalf("generate test node token hash: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
INSERT INTO nodes (name, roles, state, last_heartbeat_at, cooling_until, token_hash)
VALUES ($1, $2, $3, $4, $5, $6)`, name, roles, state, heartbeat, cooling, tokenHash); err != nil {
		t.Fatalf("create assignment test node: %v", err)
	}
	fixture.nodes = append(fixture.nodes, name)
	return name
}

func assignmentTime() time.Time {
	return time.Date(2026, 11, 11, 12, 0, 0, 123456000, time.UTC)
}
