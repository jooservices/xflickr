package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestEnrichmentsAndAssetsCRUDAndExpiry(t *testing.T) {
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
	suffix := fmt.Sprintf("enrichment-assets-%d", time.Now().UnixNano())
	nsid := suffix + "@example.invalid"
	photoID := time.Now().UnixNano()
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, $1)`, nsid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos (id, owner_nsid, last_update, visibility, raw, content_hash) VALUES ($1,$2,now(),'public','{}',decode(repeat('ab',32),'hex'))`, photoID, nsid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id = $1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid = $1`, nsid)
	})

	enrichment := domain.PhotoEnrichment{PhotoID: photoID, Kind: domain.EnrichmentCaption, Model: "caption-model", ModelVersion: "1", Payload: []byte(`{"text":"A photo"}`)}
	if err := repository.UpsertEnrichment(ctx, enrichment); err != nil {
		t.Fatalf("UpsertEnrichment(): %v", err)
	}
	enrichment.ModelVersion = "2"
	enrichment.Payload = []byte(`{"text":"Updated caption"}`)
	if err := repository.UpsertEnrichment(ctx, enrichment); err != nil {
		t.Fatalf("UpsertEnrichment() update: %v", err)
	}
	gotEnrichment, found, err := repository.GetEnrichment(ctx, photoID, domain.EnrichmentCaption, "caption-model")
	var gotPayload, wantPayload any
	if err := json.Unmarshal(gotEnrichment.Payload, &gotPayload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(enrichment.Payload, &wantPayload); err != nil {
		t.Fatal(err)
	}
	if err != nil || !found || gotEnrichment.ModelVersion != "2" || fmt.Sprint(gotPayload) != fmt.Sprint(wantPayload) || gotEnrichment.CreatedAt.IsZero() {
		t.Fatalf("GetEnrichment() = %+v, found=%v, err=%v", gotEnrichment, found, err)
	}
	if err := repository.DeleteEnrichment(ctx, photoID, domain.EnrichmentCaption, "caption-model"); err != nil {
		t.Fatalf("DeleteEnrichment(): %v", err)
	}
	if _, found, err := repository.GetEnrichment(ctx, photoID, domain.EnrichmentCaption, "caption-model"); err != nil || found {
		t.Fatalf("GetEnrichment() after delete: found=%v err=%v", found, err)
	}

	asset, err := repository.CreateAsset(ctx, domain.Asset{
		PhotoID: photoID, SizeLabel: "Original", SHA256: make([]byte, 32), Bytes: 123,
		BlobKey: suffix + "/original", ExpiresAt: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateAsset(): %v", err)
	}
	gotAsset, found, err := repository.GetAsset(ctx, asset.ID)
	if err != nil || !found || gotAsset.ID != asset.ID || gotAsset.PhotoID != photoID || gotAsset.Bytes != 123 || gotAsset.CreatedAt.IsZero() {
		t.Fatalf("GetAsset() = %+v, found=%v, err=%v", gotAsset, found, err)
	}
	asset.SizeLabel = "Small"
	asset.Bytes = 42
	asset.ExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := repository.UpdateAsset(ctx, asset); err != nil {
		t.Fatalf("UpdateAsset(): %v", err)
	}
	gotAsset, found, err = repository.GetAsset(ctx, asset.ID)
	if err != nil || !found || gotAsset.SizeLabel != "Small" || gotAsset.Bytes != 42 {
		t.Fatalf("GetAsset() after update = %+v, found=%v, err=%v", gotAsset, found, err)
	}
	asset.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	if err := repository.UpdateAsset(ctx, asset); err != nil {
		t.Fatalf("UpdateAsset() expire: %v", err)
	}
	expired, err := repository.ExpiredAssets(ctx, 1)
	if err != nil || len(expired) != 1 || expired[0].ID != asset.ID {
		t.Fatalf("ExpiredAssets() = %+v, err=%v; want asset %d", expired, err, asset.ID)
	}
	if err := repository.DeleteAsset(ctx, asset.ID); err != nil {
		t.Fatalf("DeleteAsset(): %v", err)
	}
	if _, found, err := repository.GetAsset(ctx, asset.ID); err != nil || found {
		t.Fatalf("GetAsset() after delete: found=%v err=%v", found, err)
	}
	if _, err := repository.ExpiredAssets(ctx, 0); err == nil {
		t.Fatal("ExpiredAssets() accepted a zero limit")
	}
}
