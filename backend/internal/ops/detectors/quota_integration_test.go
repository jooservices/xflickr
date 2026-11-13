package detectors_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/ops/adapters/postgres"
	"github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/ops/detectors"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestQuotaDetectorPersistsIncident(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	repository, err := postgres.NewIncidentRepository(pool)
	if err != nil {
		t.Fatalf("NewIncidentRepository() error = %v", err)
	}
	detector, err := detectors.NewQuotaDetector(app.NewIncidents(repository))
	if err != nil {
		t.Fatalf("NewQuotaDetector() error = %v", err)
	}
	keyID := time.Now().UnixNano()
	scope := "key:" + strconv.FormatInt(keyID, 10)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM incidents WHERE kind = $1 AND scope = $2", "quota_exhausted", scope); err != nil {
			t.Errorf("delete quota incident: %v", err)
		}
	})

	if err := detector.Detect(ctx, detectors.QuotaSnapshot{KeyID: keyID, Used: 125, Limit: 125}); err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	var incidentID int64
	var kind, storedScope, severity, title, state string
	var detail []byte
	if err := pool.QueryRow(ctx, `
		SELECT id, kind, scope, severity, title, detail, state
		FROM incidents WHERE kind = $1 AND scope = $2`, "quota_exhausted", scope).
		Scan(&incidentID, &kind, &storedScope, &severity, &title, &detail, &state); err != nil {
		t.Fatalf("read persisted quota incident: %v", err)
	}
	wantDetail := json.RawMessage(`{"detection":"Key quota reached its limit.","automatic_action":"No automatic remediation is defined.","human_step":"Check the key and reset time in API quota; follow key policy.","used":125,"limit":125}`)
	var gotJSON, wantJSON any
	if err := json.Unmarshal(detail, &gotJSON); err != nil {
		t.Fatalf("decode persisted incident detail: %v", err)
	}
	if err := json.Unmarshal(wantDetail, &wantJSON); err != nil {
		t.Fatalf("decode expected incident detail: %v", err)
	}
	if incidentID <= 0 || kind != "quota_exhausted" || storedScope != scope || severity != "warning" ||
		title != "API key quota exhausted" || state != "open" || !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("persisted incident = id %d, kind %q, scope %q, severity %q, title %q, state %q, detail %s",
			incidentID, kind, storedScope, severity, title, state, detail)
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source file")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	if !strings.HasPrefix(dbConfig.Name, "xflickr_wt") {
		t.Fatal("integration test requires this worktree's isolated database")
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, dbConfig)
}
