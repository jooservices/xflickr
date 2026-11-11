package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	settingshttp "github.com/jooservices/xflickr/internal/settings/adapters/http"
	"github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	"github.com/jooservices/xflickr/internal/settings/app"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

func TestFeatureFlagPostgresLifecycle(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("integration.flag.%d", time.Now().UnixNano())
	emailPrefix := fmt.Sprintf("joo87-%d-", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM feature_flags WHERE name LIKE $1`, prefix+"%"); err != nil {
			t.Errorf("delete feature flags: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE email LIKE $1`, emailPrefix+"%"); err != nil {
			t.Errorf("delete feature flag users: %v", err)
		}
	})

	repository, err := postgres.NewFlagRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	var adminID, memberID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','admin') RETURNING id`, emailPrefix+"admin@example.invalid").Scan(&adminID); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, emailPrefix+"member@example.invalid").Scan(&memberID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	flags := []domain.FeatureFlag{
		{Name: prefix + ".all", Enabled: true, Audience: domain.AudienceAll},
		{Name: prefix + ".admins", Enabled: true, Audience: domain.AudienceAdmins},
		{Name: prefix + ".users", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{memberID}},
	}
	if err := repository.UpsertFlags(ctx, flags); err != nil {
		t.Fatalf("UpsertFlags() error = %v", err)
	}
	loaded, err := repository.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags() error = %v", err)
	}
	byName := make(map[string]domain.FeatureFlag, len(loaded))
	for _, flag := range loaded {
		if len(flag.Name) >= len(prefix) && flag.Name[:len(prefix)] == prefix {
			byName[flag.Name] = flag
		}
	}
	if len(byName) != 3 || byName[prefix+".users"].UserIDs[0] != memberID || !byName[prefix+".all"].Enabled {
		t.Fatalf("stored feature flags = %+v", byName)
	}
	if admin, err := repository.UserIsAdmin(ctx, adminID); err != nil || !admin {
		t.Fatalf("admin role = %v, %v", admin, err)
	}
	if admin, err := repository.UserIsAdmin(ctx, memberID); err != nil || admin {
		t.Fatalf("member role = %v, %v", admin, err)
	}
	if admin, err := repository.UserIsAdmin(ctx, 0); err != nil || admin {
		t.Fatalf("system identity role = %v, %v", admin, err)
	}

	flags[0].Enabled = false
	if err := repository.UpsertFlags(ctx, flags[:1]); err != nil {
		t.Fatalf("upsert changed flag: %v", err)
	}
	loaded, err = repository.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags() after upsert: %v", err)
	}
	for _, flag := range loaded {
		if flag.Name == prefix+".all" && flag.Enabled {
			t.Fatal("upsert did not change enabled state")
		}
	}

	if err := repository.UpsertFlags(ctx, []domain.FeatureFlag{{Name: "INVALID", Audience: domain.AudienceAll}}); !errors.Is(err, domain.ErrInvalidFeatureFlag) {
		t.Fatalf("invalid flag error = %v", err)
	}
}

func TestFeatureFlagGetPostgres(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	name := fmt.Sprintf("integration.flag.get.%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM feature_flags WHERE name = $1`, name); err != nil {
			t.Errorf("delete feature flag fixture: %v", err)
		}
	})

	repository, err := postgres.NewFlagRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewFeatureFlags(repository)
	handler := settingshttp.Handler{Flags: service}
	flag := domain.FeatureFlag{Name: name, Enabled: false, Audience: domain.AudienceAll}
	updateFlagViaHTTP(t, handler, ctx, flag)
	first := visitFeatureFlag(t, handler, ctx, name)
	if first.Code != 200 || first.Header().Get("ETag") == "" {
		t.Fatalf("first flag response = %d %v %s", first.Code, first.Header(), first.Body.String())
	}

	flag.Enabled = true
	updateFlagViaHTTP(t, handler, ctx, flag)
	second := visitFeatureFlag(t, handler, ctx, name)
	var body struct {
		Data struct {
			Enabled bool `json:"enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if second.Code != 200 || !body.Data.Enabled || second.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatalf("updated response = %d, enabled=%v, ETag=%q (before %q)", second.Code, body.Data.Enabled, second.Header().Get("ETag"), first.Header().Get("ETag"))
	}
}

func TestFeatureFlagGetPlan(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	prefix := fmt.Sprintf("integration.plan.%d.", time.Now().UnixNano())
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	const insertFixtureSQL = `
INSERT INTO feature_flags (name, enabled, audience, user_ids)
SELECT $1 || item::text, false, 'all', '{}'::bigint[]
FROM generate_series(1, 400000) AS item`
	if tag, err := tx.Exec(ctx, insertFixtureSQL, prefix); err != nil {
		t.Fatalf("insert feature flag plan fixture: %v", err)
	} else if tag.RowsAffected() != 400000 {
		t.Fatalf("fixture rows = %d, want 400000", tag.RowsAffected())
	}
	if _, err := tx.Exec(ctx, `ANALYZE feature_flags`); err != nil {
		t.Fatalf("analyze feature flag plan fixture: %v", err)
	}
	const lookupSQL = `SELECT name, enabled, audience, user_ids FROM feature_flags WHERE name = $1`
	var planJSON []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+lookupSQL, prefix+"400000").Scan(&planJSON); err != nil {
		t.Fatalf("explain flag lookup: %v", err)
	}
	var plan []struct {
		Plan featureFlagPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		t.Fatalf("decode flag lookup plan: %v", err)
	}
	if len(plan) == 0 {
		t.Fatalf("empty flag lookup plan: %s", planJSON)
	}
	seqScan, primaryKey := inspectFeatureFlagPlan(plan[0].Plan)
	if seqScan || !primaryKey {
		t.Fatalf("flag lookup plan must use feature_flags_pkey and contain no Seq Scan: %s", planJSON)
	}
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+lookupSQL, prefix+"400000").Scan(&planJSON); err != nil {
		t.Fatalf("analyze flag lookup plan: %v", err)
	}
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) flag lookup on 400000 rows: %s", planJSON)
}

type featureFlagPlanNode struct {
	NodeType  string                `json:"Node Type"`
	IndexName string                `json:"Index Name"`
	Plans     []featureFlagPlanNode `json:"Plans"`
}

func inspectFeatureFlagPlan(node featureFlagPlanNode) (seqScan, primaryKey bool) {
	seqScan = node.NodeType == "Seq Scan"
	primaryKey = node.IndexName == "feature_flags_pkey"
	for _, child := range node.Plans {
		childSeqScan, childPrimaryKey := inspectFeatureFlagPlan(child)
		seqScan = seqScan || childSeqScan
		primaryKey = primaryKey || childPrimaryKey
	}
	return seqScan, primaryKey
}

func visitFeatureFlag(t *testing.T, handler settingshttp.Handler, ctx context.Context, name string) *httptest.ResponseRecorder {
	t.Helper()
	response, err := handler.GetAdminFlag(ctx, gen.GetAdminFlagRequestObject{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitGetAdminFlagResponse(recorder); err != nil {
		t.Fatal(err)
	}
	return recorder
}

func updateFlagViaHTTP(t *testing.T, handler settingshttp.Handler, ctx context.Context, flag domain.FeatureFlag) {
	t.Helper()
	response, err := handler.UpdateAdminFlags(ctx, gen.UpdateAdminFlagsRequestObject{Body: &gen.UpdateAdminFlagsRequest{
		Flags: []gen.FeatureFlag{{
			Name: flag.Name, Enabled: flag.Enabled, Audience: gen.FeatureFlagAudience(flag.Audience), UserIds: []int64{},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(gen.UpdateAdminFlags204Response); !ok {
		t.Fatalf("update flag response = %T", response)
	}
}
