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
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/cluster/app"
	"github.com/jooservices/xflickr/internal/cluster/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestNodeTokenAndHeartbeatLifecycle(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	name := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM nodes WHERE name=$1`, name); err != nil {
			t.Errorf("delete test node: %v", err)
		}
	})

	repository := New(pool)
	service := app.Nodes{Repository: repository}
	node, token, err := service.Create(ctx, name, []string{"crawl", "media"})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if node.Name != name || node.State != domain.StateOffline {
		t.Fatalf("created node = %+v", node)
	}
	var storedHash []byte
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM nodes WHERE name=$1`, name).Scan(&storedHash); err != nil {
		t.Fatalf("read token hash: %v", err)
	}
	if !bytes.Equal(storedHash, domain.HashNodeToken(token)) || bytes.Contains(storedHash, []byte(token)) {
		t.Fatal("database did not store the one-way token hash")
	}
	if _, err := service.AuthenticateToken(ctx, token); err != nil {
		t.Fatalf("authenticate generated token: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, testOtherToken()); !errors.Is(err, app.ErrInvalidToken) {
		t.Fatalf("unknown token error = %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	updated, err := service.Heartbeat(ctx, name, app.HeartbeatInput{Version: "v1", EgressIP: "192.0.2.44", Metrics: []byte(`{"load":0.25}`)})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if updated.State != domain.StateOnline || updated.Version != "v1" || updated.EgressIP != "192.0.2.44" || updated.LastHeartbeatAt == nil || updated.LastHeartbeatAt.Before(at.Add(-time.Second)) {
		t.Fatalf("updated node = %+v", updated)
	}
	if string(updated.Metrics) != `{"load": 0.25}` {
		t.Fatalf("metrics = %s", updated.Metrics)
	}
	if _, err := pool.Exec(ctx, `UPDATE nodes SET state='disabled' WHERE name=$1`, name); err != nil {
		t.Fatalf("disable test node: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, token); !errors.Is(err, app.ErrInvalidToken) {
		t.Fatalf("disabled node token error = %v", err)
	}
}

func TestFindDuplicateEgress(t *testing.T) {
	at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	warningInterval := pgtype.Interval{Microseconds: domain.WarningAfter.Microseconds()}

	t.Run("empty address skips lookup", func(t *testing.T) {
		pool := &fakeNodePool{row: fakeDuplicateNameRow{name: "other"}}
		name, err := (&Repository{pool: pool}).FindDuplicateEgress(t.Context(), domain.Node{Name: "current"}, at)
		if err != nil || name != "" || pool.args != nil {
			t.Fatalf("FindDuplicateEgress() = %q, %v, args %v; want no lookup", name, err, pool.args)
		}
	})

	t.Run("returns one matched node and query arguments", func(t *testing.T) {
		pool := &fakeNodePool{row: fakeDuplicateNameRow{name: "other"}}
		node := domain.Node{Name: "current", EgressIP: "192.0.2.1"}
		name, err := (&Repository{pool: pool}).FindDuplicateEgress(t.Context(), node, at)
		if err != nil || name != "other" {
			t.Fatalf("FindDuplicateEgress() = %q, %v", name, err)
		}
		if len(pool.args) != 4 || pool.args[0] != node.EgressIP || pool.args[1] != node.Name || !pool.args[2].(time.Time).Equal(at) || pool.args[3] != warningInterval {
			t.Fatalf("duplicate lookup arguments = %#v", pool.args)
		}
	})

	t.Run("no match is empty", func(t *testing.T) {
		pool := &fakeNodePool{row: fakeDuplicateNameRow{err: pgx.ErrNoRows}}
		name, err := (&Repository{pool: pool}).FindDuplicateEgress(t.Context(), domain.Node{Name: "current", EgressIP: "192.0.2.1"}, at)
		if err != nil || name != "" {
			t.Fatalf("FindDuplicateEgress() = %q, %v, want no match", name, err)
		}
	})

	t.Run("query failure is wrapped", func(t *testing.T) {
		want := errors.New("database unavailable")
		pool := &fakeNodePool{row: fakeDuplicateNameRow{err: want}}
		_, err := (&Repository{pool: pool}).FindDuplicateEgress(t.Context(), domain.Node{Name: "current", EgressIP: "192.0.2.1"}, at)
		if !errors.Is(err, want) {
			t.Fatalf("FindDuplicateEgress() error = %v, want %v", err, want)
		}
	})
}

type fakeDuplicateNameRow struct {
	name string
	err  error
}

func (row fakeDuplicateNameRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != 1 {
		return errors.New("unexpected duplicate lookup scan shape")
	}
	*dest[0].(*string) = row.name
	return nil
}

func testOtherToken() string {
	return "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
}

func TestDuplicateEgressEligibility(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree database")
	}
	pool := openDuplicateIntegrationDatabase(t)
	repository := New(pool)
	for index, test := range []struct {
		name           string
		currentIP      string
		candidateIP    string
		roles          []string
		state          string
		heartbeatAge   time.Duration
		coolingUntil   bool
		coolingExpired bool
		wantCandidate  bool
	}{
		{name: "empty IP"},
		{name: "different IP", currentIP: "198.18.2.1", candidateIP: "198.18.2.2", roles: []string{"crawl"}, state: "online"},
		{name: "wrong role", currentIP: "198.18.3.1", candidateIP: "198.18.3.1", roles: []string{"media"}, state: "online"},
		{name: "disabled", currentIP: "198.18.4.1", candidateIP: "198.18.4.1", roles: []string{"crawl"}, state: "disabled"},
		{name: "cooling", currentIP: "198.18.5.1", candidateIP: "198.18.5.1", roles: []string{"crawl"}, state: "cooling", coolingUntil: true},
		{name: "cooling just expired", currentIP: "198.18.8.1", candidateIP: "198.18.8.1", roles: []string{"crawl"}, state: "cooling", coolingUntil: true, coolingExpired: true, wantCandidate: true},
		{name: "exactly 60 seconds", currentIP: "198.18.6.1", candidateIP: "198.18.6.1", roles: []string{"crawl"}, state: "online", heartbeatAge: domain.WarningAfter, wantCandidate: true},
		{name: "older than 60 seconds", currentIP: "198.18.7.1", candidateIP: "198.18.7.1", roles: []string{"crawl"}, state: "online", heartbeatAge: domain.WarningAfter + time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			prefix := fmt.Sprintf("duplicate-eligibility-%d-%d", time.Now().UnixNano(), index)
			cleanupDuplicateFixtures(t, pool, prefix)
			at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
			var coolingUntil *time.Time
			if test.coolingUntil {
				coolingAt := at.Add(time.Minute)
				if test.coolingExpired {
					coolingAt = at.Add(-time.Second)
				}
				coolingUntil = timePointer(coolingAt)
			}
			if test.currentIP != "" {
				insertDuplicateNode(t, pool, ctx, prefix+"-candidate", test.roles, test.state, test.candidateIP, timePointer(at.Add(-test.heartbeatAge)), coolingUntil)
			}
			duplicate, err := repository.FindDuplicateEgress(ctx, domain.Node{Name: prefix + "-current", EgressIP: test.currentIP}, at)
			if err != nil {
				t.Fatalf("find duplicate egress node: %v", err)
			}
			if gotCandidate := duplicate != ""; gotCandidate != test.wantCandidate {
				t.Fatalf("duplicate node = %q, want candidate present %t", duplicate, test.wantCandidate)
			}
		})
	}
}

func TestDuplicateEgressPlanUsesIndex(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree database")
	}
	pool := openDuplicateIntegrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	prefix := fmt.Sprintf("duplicate-plan-%d", time.Now().UnixNano())
	cleanupDuplicateFixtures(t, pool, prefix)
	const fixtureSize = 400_000
	at := time.Now().UTC().Truncate(time.Microsecond)
	const egressIP = "198.19.0.1"
	insertDuplicateNode(t, pool, ctx, prefix+"-current", []string{"crawl"}, "online", egressIP, timePointer(at), nil)
	insertDuplicateNode(t, pool, ctx, prefix+"-match", []string{"crawl"}, "online", egressIP, timePointer(at), nil)
	command, err := pool.Exec(ctx, `
INSERT INTO nodes (name, roles, state, egress_ip, last_heartbeat_at, token_hash)
SELECT $1 || '-' || lpad(fixture.id::text, 6, '0'), ARRAY['crawl'], 'online',
       '198.18.0.0'::inet + fixture.id, $3,
       decode(md5($1 || ':' || fixture.id::text) || md5('token:' || $1 || ':' || fixture.id::text), 'hex')
FROM generate_series(1, $2::integer) AS fixture(id)`, prefix, fixtureSize, at)
	if err != nil {
		t.Fatalf("create %d index plan fixtures: %v", fixtureSize, err)
	}
	if command.RowsAffected() != fixtureSize {
		t.Fatalf("inserted %d index plan fixtures, want %d", command.RowsAffected(), fixtureSize)
	}
	if _, err := pool.Exec(ctx, `ANALYZE nodes`); err != nil {
		t.Fatalf("analyze duplicate egress fixtures: %v", err)
	}

	warningInterval := pgtype.Interval{Microseconds: domain.WarningAfter.Microseconds()}
	var rawPlan []byte
	if err := pool.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+findDuplicateEgressSQL,
		egressIP, prefix+"-current", at, warningInterval).Scan(&rawPlan); err != nil {
		t.Fatalf("explain duplicate egress lookup: %v", err)
	}
	assertDuplicateEgressPlanUsesIndex(t, rawPlan)
}

func openDuplicateIntegrationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test source path")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}

func insertDuplicateNode(t *testing.T, pool *pgxpool.Pool, ctx context.Context, name string, roles []string, state, egressIP string, heartbeatAt, coolingUntil *time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO nodes (name, roles, state, egress_ip, last_heartbeat_at, cooling_until, token_hash)
VALUES ($1, $2, $3, NULLIF($4, '')::inet, $5, $6,
        decode(md5($1) || md5($1 || ':token'), 'hex'))`, name, roles, state, egressIP, heartbeatAt, coolingUntil); err != nil {
		t.Fatalf("insert duplicate egress fixture %q: %v", name, err)
	}
}

func cleanupDuplicateFixtures(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, `DELETE FROM incidents WHERE kind = 'duplicate_egress_ip' AND scope LIKE $1 || '%'`, prefix); err != nil {
			t.Errorf("delete duplicate egress incidents: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM nodes WHERE name LIKE $1 || '%'`, prefix); err != nil {
			t.Errorf("delete duplicate egress nodes: %v", err)
		}
	})
}

func timePointer(value time.Time) *time.Time {
	return &value
}

type duplicateEgressPlanDocument []struct {
	Plan duplicateEgressPlanNode `json:"Plan"`
}

type duplicateEgressPlanNode struct {
	NodeType  string                    `json:"Node Type"`
	IndexName string                    `json:"Index Name"`
	Plans     []duplicateEgressPlanNode `json:"Plans"`
}

func assertDuplicateEgressPlanUsesIndex(t *testing.T, rawPlan []byte) {
	t.Helper()
	var document duplicateEgressPlanDocument
	if err := json.Unmarshal(rawPlan, &document); err != nil {
		t.Fatalf("decode duplicate egress query plan: %v", err)
	}
	if len(document) != 1 {
		t.Fatalf("duplicate egress plan has %d roots, want one", len(document))
	}
	foundIndex := false
	var walk func(duplicateEgressPlanNode)
	walk = func(node duplicateEgressPlanNode) {
		if node.NodeType == "Seq Scan" {
			t.Errorf("duplicate egress plan contains Seq Scan: %s", rawPlan)
		}
		if node.IndexName == "nodes_duplicate_egress_idx" {
			foundIndex = true
		}
		for _, child := range node.Plans {
			walk(child)
		}
	}
	walk(document[0].Plan)
	if !foundIndex {
		t.Errorf("duplicate egress plan does not use nodes_duplicate_egress_idx: %s", rawPlan)
	}
}
