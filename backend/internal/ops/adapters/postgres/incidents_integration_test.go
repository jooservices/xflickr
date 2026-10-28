package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/ops/adapters/postgres"
	"github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/ops/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestIncidentPostgresLifecycle(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	repository, err := postgres.NewIncidentRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	manager := app.NewIncidents(repository)
	kind := fmt.Sprintf("integration.incident.%d", time.Now().UnixNano())
	scope := "node-158"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM outbox_events WHERE topic = $1 AND key IN (SELECT id::text FROM incidents WHERE kind = $2)", domain.NotificationRequestedTopic, kind); err != nil {
			t.Errorf("delete incident outbox events: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM incidents WHERE kind = $1", kind); err != nil {
			t.Errorf("delete incidents: %v", err)
		}
	})

	first, err := manager.Open(ctx, domain.Incident{
		Kind: kind, Scope: scope, Severity: domain.SeverityCritical,
		Title: "Crawl stalled", Detail: json.RawMessage(`{"pending":7}`),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if first.ID <= 0 || first.State != domain.IncidentOpen || first.Count != 1 {
		t.Fatalf("first incident = %+v", first)
	}

	repeated, err := manager.Open(ctx, domain.Incident{
		Kind: kind, Scope: scope, Severity: domain.SeverityWarning,
		Title: "Changed title", Detail: json.RawMessage(`{"pending":8}`),
	})
	if err != nil {
		t.Fatalf("Open() repeated error = %v", err)
	}
	if repeated.ID != first.ID || repeated.Count != 2 || repeated.Severity != domain.SeverityCritical ||
		repeated.Title != first.Title || string(repeated.Detail) != string(first.Detail) {
		t.Fatalf("deduplicated incident = %+v, want original evidence and count 2", repeated)
	}

	var notifications int
	var payload []byte
	if err := pool.QueryRow(ctx, "SELECT count(*), (array_agg(payload ORDER BY id))[1] FROM outbox_events WHERE topic = $1 AND key = $2",
		domain.NotificationRequestedTopic, fmt.Sprint(first.ID)).Scan(&notifications, &payload); err != nil {
		t.Fatalf("read notification event: %v", err)
	}
	if notifications != 1 {
		t.Fatalf("critical notification count = %d, want one", notifications)
	}
	var request domain.NotificationRequested
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode notification event: %v", err)
	}
	if request.Kind != domain.NotificationIncident || request.IncidentID != first.ID ||
		request.Severity != domain.SeverityCritical || len(request.Recipients) != 1 || request.Recipients[0] != "admins" ||
		len(request.Channels) != 1 || request.Channels[0] != "alert" {
		t.Fatalf("notification request = %+v", request)
	}

	acked, err := manager.Ack(ctx, first.ID, 17)
	if err != nil || acked.State != domain.IncidentAcked || acked.AckedBy == nil || *acked.AckedBy != 17 {
		t.Fatalf("Ack() = %+v, %v", acked, err)
	}
	closed, err := manager.Close(ctx, first.ID)
	if err != nil || closed.State != domain.IncidentClosed || closed.ClosedAt == nil {
		t.Fatalf("Close() = %+v, %v", closed, err)
	}
	if _, err := manager.Ack(ctx, first.ID, 17); err == nil {
		t.Fatal("Ack() after close succeeded")
	}

	warning, err := manager.Open(ctx, domain.Incident{
		Kind: kind, Scope: "node-159", Severity: domain.SeverityWarning, Title: "Queue slow",
	})
	if err != nil {
		t.Fatalf("Open() warning error = %v", err)
	}
	if closedCount, err := manager.AutoClose(ctx, kind, "node-159"); err != nil || closedCount != 1 {
		t.Fatalf("AutoClose() = %d, %v, want one", closedCount, err)
	}
	if closedCount, err := manager.AutoClose(ctx, kind, "node-159"); err != nil || closedCount != 0 {
		t.Fatalf("AutoClose() repeated = %d, %v, want zero", closedCount, err)
	}
	reopened, err := manager.Open(ctx, domain.Incident{
		Kind: kind, Scope: "node-159", Severity: domain.SeverityWarning, Title: "Queue slow again",
	})
	if err != nil || reopened.ID == warning.ID || reopened.Count != 1 {
		t.Fatalf("Open() after auto-close = %+v, %v", reopened, err)
	}
	if _, err := manager.Ack(ctx, reopened.ID, 18); err != nil {
		t.Fatalf("Ack() reopened warning error = %v", err)
	}
	if closedCount, err := manager.AutoClose(ctx, kind, "node-159"); err != nil || closedCount != 1 {
		t.Fatalf("AutoClose() acknowledged incident = %d, %v, want one", closedCount, err)
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
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, dbConfig)
}
