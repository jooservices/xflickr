package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	heartbeatclient "github.com/jooservices/xflickr/internal/cluster/adapters/heartbeat"
	clusterhttp "github.com/jooservices/xflickr/internal/cluster/adapters/http"
	clusterpostgres "github.com/jooservices/xflickr/internal/cluster/adapters/postgres"
	clusterapp "github.com/jooservices/xflickr/internal/cluster/app"
	clusterdomain "github.com/jooservices/xflickr/internal/cluster/domain"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
)

func TestHeartbeatClientPersistsNode(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree database")
	}
	pool := openHeartbeatIntegrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	service := clusterapp.Nodes{Repository: clusterpostgres.New(pool)}
	name := fmt.Sprintf("heartbeat-client-%d", time.Now().UnixNano())
	_, token, err := service.Create(ctx, name, []string{"crawl"})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	cleanupHeartbeatNodes(t, pool, []string{name})

	routers := heartbeatTestRouters(service)
	server := httptest.NewServer(routers.internal)
	defer server.Close()
	client, err := heartbeatclient.New(server.URL, token, &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	input := heartbeatclient.Input{Version: "node-agent-1.0", EgressIP: "198.51.100.14", Metrics: json.RawMessage(`{"cpu":0.75,"memory":128}`)}
	result, err := client.Send(ctx, input)
	if err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	if result.Name != name || result.State != string(clusterdomain.StateOnline) || result.LastHeartbeatAt.IsZero() {
		t.Fatalf("heartbeat result = %+v", result)
	}

	var storedName, version, egressIP, state string
	var lastHeartbeatAt time.Time
	var metrics map[string]float64
	if err := pool.QueryRow(ctx, `
SELECT name, version, COALESCE(host(egress_ip), ''), state, last_heartbeat_at, metrics
FROM nodes WHERE name = $1`, name).Scan(&storedName, &version, &egressIP, &state, &lastHeartbeatAt, &metrics); err != nil {
		t.Fatalf("read persisted node: %v", err)
	}
	if storedName != name || version != input.Version || egressIP != input.EgressIP || state != string(clusterdomain.StateOnline) {
		t.Fatalf("stored node fields = name %q, version %q, egress IP %q, state %q", storedName, version, egressIP, state)
	}
	if !lastHeartbeatAt.Equal(result.LastHeartbeatAt) || metrics["cpu"] != 0.75 || metrics["memory"] != 128 {
		t.Fatalf("stored timestamp or metrics = %s, %+v; response timestamp %s", lastHeartbeatAt, metrics, result.LastHeartbeatAt)
	}

	invalidClient, err := heartbeatclient.New(server.URL, token+"-invalid", &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	invalidInput := heartbeatclient.Input{Version: "must-not-persist", EgressIP: "203.0.113.9", Metrics: json.RawMessage(`{"cpu":0.99}`)}
	if _, err := invalidClient.Send(ctx, invalidInput); err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("invalid token error = %v", err)
	}
	var afterVersion, afterEgressIP, afterState string
	var afterHeartbeatAt time.Time
	var afterMetrics map[string]float64
	if err := pool.QueryRow(ctx, `
SELECT version, COALESCE(host(egress_ip), ''), state, last_heartbeat_at, metrics
FROM nodes WHERE name = $1`, name).Scan(&afterVersion, &afterEgressIP, &afterState, &afterHeartbeatAt, &afterMetrics); err != nil {
		t.Fatalf("read node after invalid token: %v", err)
	}
	if afterVersion != version || afterEgressIP != egressIP || afterState != state || !afterHeartbeatAt.Equal(lastHeartbeatAt) || afterMetrics["cpu"] != metrics["cpu"] {
		t.Fatalf("invalid token changed node: version %q, IP %q, state %q, at %s, metrics %+v", afterVersion, afterEgressIP, afterState, afterHeartbeatAt, afterMetrics)
	}
}

func TestHeartbeatQueriesUseIndexes(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree database")
	}
	pool := openHeartbeatIntegrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	const fixtureSize = 10_000
	prefix := fmt.Sprintf("heartbeat-plan-%d", time.Now().UnixNano())
	names := make([]string, fixtureSize)
	name := fmt.Sprintf("%s-00000", prefix)
	names[0] = name
	service := clusterapp.Nodes{Repository: clusterpostgres.New(pool)}
	_, token, err := service.Create(ctx, name, []string{"crawl"})
	if err != nil {
		t.Fatalf("create plan target node: %v", err)
	}
	for index := 1; index < fixtureSize; index++ {
		names[index] = fmt.Sprintf("%s-%05d", prefix, index)
	}
	cleanupHeartbeatNodes(t, pool, names)

	command, err := pool.Exec(ctx, `
INSERT INTO nodes (name, roles, state, token_hash)
SELECT $1 || '-' || lpad(fixture.id::text, 5, '0'), ARRAY['crawl'], 'offline',
       decode(md5($1 || ':' || fixture.id::text) || md5('token:' || $1 || ':' || fixture.id::text), 'hex')
FROM generate_series(1, $2::integer) AS fixture(id)`, prefix, fixtureSize-1)
	if err != nil {
		t.Fatalf("create remaining plan fixtures: %v", err)
	}
	if command.RowsAffected() != fixtureSize-1 {
		t.Fatalf("created %d plan fixtures, want %d", command.RowsAffected(), fixtureSize-1)
	}
	if _, err := pool.Exec(ctx, `ANALYZE nodes`); err != nil {
		t.Fatalf("analyze node fixtures: %v", err)
	}

	const lookupQuery = `SELECT name, roles, region, version, COALESCE(host(egress_ip), ''), state,
       cooling_until, last_heartbeat_at, metrics::text
FROM nodes WHERE token_hash = $1 AND state <> 'disabled'`
	var lookupPlan []byte
	if err := pool.QueryRow(ctx, `EXPLAIN (FORMAT JSON) `+lookupQuery, clusterdomain.HashNodeToken(token)).Scan(&lookupPlan); err != nil {
		t.Fatalf("explain token lookup: %v", err)
	}
	assertHeartbeatPlanUsesIndex(t, lookupPlan, "nodes_token_hash_key")

	const updateQuery = `UPDATE nodes SET version = $2, egress_ip = NULLIF($3, '')::inet,
    metrics = $4::jsonb, last_heartbeat_at = $5,
    state = CASE WHEN cooling_until > $5 THEN 'cooling' ELSE 'online' END
WHERE name = $1 AND state <> 'disabled'
RETURNING name, roles, region, version, COALESCE(host(egress_ip), ''), state,
          cooling_until, last_heartbeat_at, metrics::text`
	var updatePlan []byte
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := pool.QueryRow(ctx, `EXPLAIN (FORMAT JSON) `+updateQuery, name, "v1", "192.0.2.5", `{"load":0.25}`, at).Scan(&updatePlan); err != nil {
		t.Fatalf("explain heartbeat update: %v", err)
	}
	assertHeartbeatPlanUsesIndex(t, updatePlan, "nodes_pkey")
}

func openHeartbeatIntegrationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test source path")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	databaseConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, databaseConfig)
}

func cleanupHeartbeatNodes(t *testing.T, pool *pgxpool.Pool, names []string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, `DELETE FROM nodes WHERE name = ANY($1)`, names); err != nil {
			t.Errorf("delete heartbeat test nodes: %v", err)
		}
	})
}

func heartbeatTestRouters(service clusterapp.Nodes) apiHandlerSet {
	return newAPIRoutersWithServices(
		identityhttp.Handler{}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{},
		crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{}, sharinghttp.Handler{}, mediahttp.DownloadHandler{},
		clusterhttp.Handler{Service: service},
	)
}

type heartbeatPlanDocument []struct {
	Plan heartbeatPlanNode `json:"Plan"`
}

type heartbeatPlanNode struct {
	NodeType  string              `json:"Node Type"`
	IndexName string              `json:"Index Name"`
	Plans     []heartbeatPlanNode `json:"Plans"`
}

func assertHeartbeatPlanUsesIndex(t *testing.T, rawPlan []byte, wantIndex string) {
	t.Helper()
	var document heartbeatPlanDocument
	if err := json.Unmarshal(rawPlan, &document); err != nil {
		t.Fatalf("decode query plan: %v", err)
	}
	if len(document) != 1 {
		t.Fatalf("query plan has %d roots, want one", len(document))
	}
	foundIndex := false
	var walk func(heartbeatPlanNode)
	walk = func(node heartbeatPlanNode) {
		if node.NodeType == "Seq Scan" {
			t.Errorf("query plan contains Seq Scan: %s", rawPlan)
		}
		if node.IndexName == wantIndex {
			foundIndex = true
		}
		for _, child := range node.Plans {
			walk(child)
		}
	}
	walk(document[0].Plan)
	if !foundIndex {
		t.Errorf("query plan does not use %s: %s", wantIndex, rawPlan)
	}
}
