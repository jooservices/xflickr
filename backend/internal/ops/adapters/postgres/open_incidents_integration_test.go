package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/ops/adapters/postgres"
	"github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/ops/domain"
)

func TestIncidentOpenListFiltersAndPagesSafely(t *testing.T) {
	pool := integrationPool(t)
	ctx := t.Context()
	commandRepository, err := postgres.NewIncidentRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	kind := fmt.Sprintf("integration.open-list.%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM incidents WHERE kind = $1", kind); err != nil {
			t.Errorf("delete open-list fixtures: %v", err)
		}
	})
	now := time.Date(2026, 11, 12, 2, 0, 0, 123456000, time.UTC)
	manager := app.NewIncidentsWithClock(commandRepository, func() time.Time { return now })
	for _, scope := range []string{"first", "second", "third", "acknowledged"} {
		if _, err := manager.Open(ctx, domain.Incident{
			Kind: kind, Scope: scope, Severity: domain.SeverityWarning,
			Title: "Unapproved title must not be returned", Detail: json.RawMessage(`{"token":"never expose"}`),
		}); err != nil {
			t.Fatalf("open %s fixture: %v", scope, err)
		}
	}
	var acknowledgedID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM incidents WHERE kind = $1 AND scope = $2", kind, "acknowledged").Scan(&acknowledgedID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Ack(ctx, acknowledgedID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(ctx, domain.Incident{
		Kind: kind, Scope: "first", Severity: domain.SeverityCritical,
		Title: "Replacement title is ignored", Detail: json.RawMessage(`{"key":"secret"}`),
	}); err != nil {
		t.Fatal(err)
	}

	reader, err := postgres.NewOpenIncidentRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	service, err := app.NewOpenIncidents(reader)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.List(ctx, app.IncidentListQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.List(ctx, app.IncidentListQuery{Limit: 2, After: first.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || len(second.Items) != 1 || second.Next != nil {
		t.Fatalf("pages lengths/next = %d/%d/%+v", len(first.Items), len(second.Items), second.Next)
	}
	if first.Items[0].ID <= first.Items[1].ID || first.Items[1].ID <= second.Items[0].ID {
		t.Fatalf("equal-time items were not ordered by descending ID: %v, %v", first.Items, second.Items)
	}
	if first.Items[0].ID == acknowledgedID || first.Items[1].ID == acknowledgedID || second.Items[0].ID == acknowledgedID {
		t.Fatalf("acknowledged incident appeared in open list: %v / %v", first.Items, second.Items)
	}
	if second.Items[0].Count != 2 || second.Items[0].Severity != domain.SeverityWarning {
		t.Fatalf("deduplicated incident summary = %+v", second.Items[0])
	}
}
