package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestNeedsEnrichmentPostgres(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	var reader app.EnrichmentReader = repository
	freshness := app.EnrichmentFreshness{Reader: reader}

	suffix := fmt.Sprintf("enrichment-freshness-%d", time.Now().UnixNano())
	nsid := suffix + "@example.invalid"
	photoID := time.Now().UnixNano()
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, $1)`, nsid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM photos WHERE id = $1`, photoID); err != nil {
			t.Errorf("delete fixture photo: %v", err)
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM people WHERE nsid = $1`, nsid); err != nil {
			t.Errorf("delete fixture person: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO photos (id, owner_nsid, last_update, visibility, raw, content_hash)
		VALUES ($1, $2, now(), 'public', '{}', decode(repeat('ab', 32), 'hex'))`, photoID, nsid); err != nil {
		t.Fatal(err)
	}

	old := domain.PhotoEnrichment{PhotoID: photoID, Kind: domain.EnrichmentCaption, Model: "caption-old", ModelVersion: "v1", Payload: []byte(`{"caption":"Original caption"}`)}
	current := domain.PhotoEnrichment{PhotoID: photoID, Kind: domain.EnrichmentCaption, Model: "caption-current", ModelVersion: "v2", Payload: []byte(`{"caption":"Current caption"}`)}
	meta := domain.PhotoEnrichment{PhotoID: photoID, Kind: domain.EnrichmentEmbeddingMeta, Model: "embed-current", ModelVersion: "e1", Payload: []byte(`{"dimensions":1024}`)}
	for _, enrichment := range []domain.PhotoEnrichment{old, current, meta} {
		if err := repository.UpsertEnrichment(ctx, enrichment); err != nil {
			t.Fatalf("UpsertEnrichment(%q): %v", enrichment.Model, err)
		}
	}

	for _, test := range []struct {
		name    string
		kind    string
		model   string
		version string
		want    bool
	}{
		{name: "old model version", kind: "caption", model: "caption-old", version: "v2", want: true},
		{name: "matching version", kind: "caption", model: "caption-current", version: "v2"},
		{name: "only another model exists", kind: "caption", model: "caption-missing", version: "v2", want: true},
		{name: "kind has no result", kind: "embedding_meta", model: "embed-missing", version: "e1", want: true},
		{name: "matching embedding version", kind: "embedding_meta", model: "embed-current", version: "e1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := freshness.NeedsEnrichment(ctx, photoID, test.kind, test.model, test.version)
			if err != nil || got != test.want {
				t.Fatalf("NeedsEnrichment() = %v, %v; want %v, nil", got, err, test.want)
			}
		})
	}

	stored, found, err := repository.GetEnrichment(ctx, photoID, old.Kind, old.Model)
	if err != nil || !found || stored.ModelVersion != old.ModelVersion {
		t.Fatalf("old enrichment changed: %+v, found=%v, err=%v", stored, found, err)
	}
	var storedPayload, oldPayload any
	if err := json.Unmarshal(stored.Payload, &storedPayload); err != nil {
		t.Fatalf("decode stored old payload: %v", err)
	}
	if err := json.Unmarshal(old.Payload, &oldPayload); err != nil {
		t.Fatalf("decode original old payload: %v", err)
	}
	if !reflect.DeepEqual(storedPayload, oldPayload) {
		t.Fatalf("old payload changed: got %v, want %v", storedPayload, oldPayload)
	}
}

// TestEnrichmentFreshnessPlan proves the freshness lookup is served by the primary key at a realistic size. It builds
// its own probe table with photo_enrichments' columns and indexes (no foreign keys), so it needs no prepared fixture.
func TestEnrichmentFreshnessPlan(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	const probe = "enrichment_freshness_plan_probe"
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS `+probe); err != nil {
		t.Fatalf("drop an old probe table: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS `+probe) })
	if _, err := pool.Exec(ctx, `CREATE TABLE `+probe+` (LIKE photo_enrichments INCLUDING ALL)`); err != nil {
		t.Fatalf("create the probe table: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO `+probe+` (photo_id, kind, model, model_version, payload)
		SELECT id, 'caption', 'model', 'v1', '{}'::jsonb FROM generate_series(1, 400000) AS id`); err != nil {
		t.Fatalf("fill the probe table: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE `+probe); err != nil {
		t.Fatalf("ANALYZE the probe table: %v", err)
	}

	query := `EXPLAIN (FORMAT JSON)
		SELECT photo_id, kind, model, model_version, payload, created_at
		FROM ` + probe + ` WHERE photo_id = $1 AND kind = $2 AND model = $3`
	var encoded []byte
	if err := pool.QueryRow(ctx, query, int64(17), "caption", "model").Scan(&encoded); err != nil {
		t.Fatalf("EXPLAIN freshness query: %v", err)
	}
	var result []struct {
		Plan explainPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("plan has %d roots; want 1", len(result))
	}
	indexFound := false
	if err := inspectEnrichmentPlan(result[0].Plan, probe+"_pkey", &indexFound); err != nil {
		t.Fatal(err)
	}
	if !indexFound {
		t.Fatalf("plan does not use %s_pkey", probe)
	}
}

type explainPlanNode struct {
	NodeType  string            `json:"Node Type"`
	IndexName string            `json:"Index Name"`
	Plans     []explainPlanNode `json:"Plans"`
}

func inspectEnrichmentPlan(node explainPlanNode, wantIndex string, indexFound *bool) error {
	if node.NodeType == "Seq Scan" {
		return fmt.Errorf("plan contains unexpected node %q", node.NodeType)
	}
	if node.IndexName == wantIndex {
		*indexFound = true
	}
	for _, child := range node.Plans {
		if err := inspectEnrichmentPlan(child, wantIndex, indexFound); err != nil {
			return err
		}
	}
	return nil
}
