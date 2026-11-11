package roles

import (
	"context"
	"encoding/json"
	"errors"
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
	opsdomain "github.com/jooservices/xflickr/internal/ops/domain"
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

func TestDuplicateEgressHeartbeat(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree database")
	}
	pool := openHeartbeatIntegrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("duplicate-heartbeat-%d", time.Now().UnixNano())
	currentName := prefix + "-z-current"
	duplicateName := prefix + "-a-duplicate"
	wantScope := fmt.Sprintf("%d:%s|%d:%s", len(duplicateName), duplicateName, len(currentName), currentName)
	const egressIP = "198.18.255.1"
	at := time.Now().UTC().Truncate(time.Microsecond)
	cleanupHeartbeatNodes(t, pool, []string{currentName, duplicateName})
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM incidents WHERE kind = 'duplicate_egress_ip' AND scope = $1`, wantScope); err != nil {
			t.Errorf("delete duplicate egress incident: %v", err)
		}
	})
	insertHeartbeatNode(t, pool, ctx, currentName, []string{"crawl"}, "offline", egressIP, nil)
	duplicateHeartbeat := at.Add(-clusterdomain.WarningAfter)
	insertHeartbeatNode(t, pool, ctx, duplicateName, []string{"crawl"}, "online", egressIP, &duplicateHeartbeat)

	service, err := buildNodeService(pool)
	if err != nil {
		t.Fatalf("build node service: %v", err)
	}
	service.Now = func() time.Time { return at }
	for range 2 {
		if _, err := service.Heartbeat(ctx, currentName, clusterapp.HeartbeatInput{EgressIP: egressIP}); err != nil {
			t.Fatalf("record heartbeat: %v", err)
		}
	}

	var gotScope, severity, title, state, detail string
	var count int64
	if err := pool.QueryRow(ctx, `
SELECT scope, severity, title, state, detail::text, count
FROM incidents
	WHERE kind = 'duplicate_egress_ip' AND scope = $1`, wantScope).Scan(&gotScope, &severity, &title, &state, &detail, &count); err != nil {
		t.Fatalf("read duplicate egress incident: %v", err)
	}
	if gotScope != wantScope || severity != string(opsdomain.SeverityWarning) || title != "Duplicate egress IP" || state != string(opsdomain.IncidentOpen) || detail != "{}" || count != 2 {
		t.Fatalf("duplicate incident = scope %q, severity %q, title %q, state %q, detail %s, count %d", gotScope, severity, title, state, detail, count)
	}
}

func TestBuildNodeServiceWiresDuplicateChecker(t *testing.T) {
	service, err := buildNodeService(&pgxpool.Pool{})
	if err != nil {
		t.Fatalf("buildNodeService() error = %v", err)
	}
	if service.Repository == nil || service.DuplicateChecker == nil || service.Logger == nil {
		t.Fatalf("buildNodeService() dependencies = %+v", service)
	}
}

func TestDuplicateEgressCheckerEligibility(t *testing.T) {
	at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	fresh := at.Add(-time.Second)
	for _, test := range []struct {
		name         string
		node         clusterdomain.Node
		coolingUntil *time.Time
	}{
		{name: "empty IP", node: clusterdomain.Node{Name: "current", State: clusterdomain.StateOnline, Roles: []string{"crawl"}}},
		{name: "role missing", node: clusterdomain.Node{Name: "current", EgressIP: "198.18.0.1", State: clusterdomain.StateOnline, LastHeartbeatAt: &fresh}},
		{name: "cooling", node: clusterdomain.Node{Name: "current", EgressIP: "198.18.0.1", State: clusterdomain.StateOnline, Roles: []string{"crawl"}, LastHeartbeatAt: &fresh}, coolingUntil: timePointer(at.Add(time.Minute))},
		{name: "stale heartbeat", node: clusterdomain.Node{Name: "current", EgressIP: "198.18.0.1", State: clusterdomain.StateOnline, Roles: []string{"crawl"}, LastHeartbeatAt: timePointer(at.Add(-clusterdomain.WarningAfter - time.Second))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.node.CoolingUntil = test.coolingUntil
			finder := &duplicateEgressFinderFake{name: "other"}
			opener := &duplicateIncidentOpenerFake{}
			checker := duplicateEgressChecker{nodes: finder, incidents: opener}
			if err := checker.CheckDuplicate(t.Context(), test.node, at); err != nil {
				t.Fatalf("CheckDuplicate() error = %v", err)
			}
			if finder.calls != 0 || opener.calls != 0 {
				t.Fatalf("ineligible heartbeat made %d lookups and %d incident opens", finder.calls, opener.calls)
			}
		})
	}
}

func TestDuplicateEgressCheckerOpensStableIncident(t *testing.T) {
	at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	node := clusterdomain.Node{
		Name:            "node-z",
		Roles:           []string{"crawl"},
		EgressIP:        "198.18.0.1",
		State:           clusterdomain.StateOnline,
		LastHeartbeatAt: timePointer(at),
	}
	finder := &duplicateEgressFinderFake{name: "node-a"}
	opener := &duplicateIncidentOpenerFake{}
	checker := duplicateEgressChecker{nodes: finder, incidents: opener}
	if err := checker.CheckDuplicate(t.Context(), node, at); err != nil {
		t.Fatal(err)
	}
	if finder.calls != 1 || finder.node.Name != node.Name || !finder.at.Equal(at) {
		t.Fatalf("duplicate lookup received node %+v at %s", finder.node, finder.at)
	}
	incident := opener.incident
	if opener.calls != 1 || incident.Kind != "duplicate_egress_ip" || incident.Scope != `6:node-a|6:node-z` || incident.Severity != opsdomain.SeverityWarning || incident.Title != "Duplicate egress IP" || string(incident.Detail) != "{}" {
		t.Fatalf("opened incident = %+v", incident)
	}
}

func TestDuplicateEgressCheckerScopesDoNotCollide(t *testing.T) {
	at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	fresh := at.Add(-time.Second)
	pairs := []struct {
		name      string
		current   string
		duplicate string
		wantScope string
	}{
		{name: "pipe in duplicate name", current: "a", duplicate: "b|c", wantScope: `1:a|3:b|c`},
		{name: "pipe in current name", current: "a|b", duplicate: "c", wantScope: `3:a|b|1:c`},
	}
	seen := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			node := clusterdomain.Node{
				Name:            pair.current,
				Roles:           []string{"crawl"},
				EgressIP:        "198.18.0.1",
				State:           clusterdomain.StateOnline,
				LastHeartbeatAt: &fresh,
			}
			opener := &duplicateIncidentOpenerFake{}
			checker := duplicateEgressChecker{
				nodes:     &duplicateEgressFinderFake{name: pair.duplicate},
				incidents: opener,
			}
			if err := checker.CheckDuplicate(t.Context(), node, at); err != nil {
				t.Fatalf("CheckDuplicate() error = %v", err)
			}
			if opener.calls != 1 || opener.incident.Scope != pair.wantScope {
				t.Fatalf("incident scope = %q, calls = %d, want %q", opener.incident.Scope, opener.calls, pair.wantScope)
			}
			if _, exists := seen[opener.incident.Scope]; exists {
				t.Fatalf("distinct node pair reused incident scope %q", opener.incident.Scope)
			}
			seen[opener.incident.Scope] = struct{}{}
		})
	}
}

func TestDuplicateEgressCheckerLeavesNoMatchAlone(t *testing.T) {
	at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	node := clusterdomain.Node{Name: "current", Roles: []string{"crawl"}, EgressIP: "198.18.0.1", State: clusterdomain.StateOnline, LastHeartbeatAt: &at}
	opener := &duplicateIncidentOpenerFake{}
	checker := duplicateEgressChecker{nodes: &duplicateEgressFinderFake{}, incidents: opener}
	if err := checker.CheckDuplicate(t.Context(), node, at); err != nil {
		t.Fatalf("CheckDuplicate() error = %v", err)
	}
	if opener.calls != 0 {
		t.Fatalf("incident opens = %d, want none without a duplicate", opener.calls)
	}
}

func TestDuplicateEgressCheckerReturnsDependencyErrors(t *testing.T) {
	at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	node := clusterdomain.Node{Name: "current", Roles: []string{"crawl"}, EgressIP: "198.18.0.1", State: clusterdomain.StateOnline, LastHeartbeatAt: &at}
	want := errors.New("dependency unavailable")
	t.Run("lookup", func(t *testing.T) {
		checker := duplicateEgressChecker{nodes: &duplicateEgressFinderFake{err: want}, incidents: &duplicateIncidentOpenerFake{}}
		if err := checker.CheckDuplicate(t.Context(), node, at); !errors.Is(err, want) {
			t.Fatalf("CheckDuplicate() error = %v, want %v", err, want)
		}
	})
	t.Run("incident open", func(t *testing.T) {
		checker := duplicateEgressChecker{
			nodes:     &duplicateEgressFinderFake{name: "other"},
			incidents: &duplicateIncidentOpenerFake{err: want},
		}
		if err := checker.CheckDuplicate(t.Context(), node, at); !errors.Is(err, want) {
			t.Fatalf("CheckDuplicate() error = %v, want %v", err, want)
		}
	})
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

func insertHeartbeatNode(t *testing.T, pool *pgxpool.Pool, ctx context.Context, name string, roles []string, state, egressIP string, heartbeatAt *time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO nodes (name, roles, state, egress_ip, last_heartbeat_at, token_hash)
VALUES ($1, $2, $3, NULLIF($4, '')::inet, $5,
        decode(md5($1) || md5($1 || ':token'), 'hex'))`, name, roles, state, egressIP, heartbeatAt); err != nil {
		t.Fatalf("insert heartbeat fixture %q: %v", name, err)
	}
}

type duplicateEgressFinderFake struct {
	name  string
	err   error
	calls int
	node  clusterdomain.Node
	at    time.Time
}

func (finder *duplicateEgressFinderFake) FindDuplicateEgress(_ context.Context, node clusterdomain.Node, at time.Time) (string, error) {
	finder.calls++
	finder.node = node
	finder.at = at
	return finder.name, finder.err
}

type duplicateIncidentOpenerFake struct {
	incident opsdomain.Incident
	err      error
	calls    int
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func (opener *duplicateIncidentOpenerFake) Open(_ context.Context, incident opsdomain.Incident) (opsdomain.Incident, error) {
	opener.calls++
	opener.incident = incident
	return incident, opener.err
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
