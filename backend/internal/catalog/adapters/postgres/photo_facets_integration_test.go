package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestPhotoFacetWritesAndStatusAreAtomic(t *testing.T) {
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
	suffix := fmt.Sprintf("facet-%d", time.Now().UnixNano())
	photoID := time.Now().UnixNano() % 1_000_000_000
	updatedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid,username) VALUES ($1,$1)`, suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos (id,owner_nsid,last_update,visibility,sizes_status,sizes_checked_at,raw,content_hash)
		VALUES ($1,$2,$3,'public','confirmed',$3,'{}',decode(repeat('ab',32),'hex'))`, photoID, suffix, updatedAt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, suffix)
	})

	source, exists, err := repository.LoadPhotoFacetSource(ctx, photoID)
	if err != nil || !exists || source.SizesStatus != "confirmed" || source.SizesCheckedAt == nil {
		t.Fatalf("LoadPhotoFacetSource()=%+v exists=%t err=%v", source, exists, err)
	}
	fetchedAt := updatedAt.Add(time.Minute)
	if err := repository.SetPhotoFacet(ctx, photoID, "contexts", []byte(`{"set":[{"id":"set-1","title":"original"}],"pool":[]}`), updatedAt, fetchedAt); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetPhotoFacet(ctx, photoID, "contexts", []byte(`{"set":[{"id":"set-2","title":"new"},{"title":"invalid"}],"pool":[]}`), updatedAt, fetchedAt.Add(time.Minute)); err == nil {
		t.Fatal("SetPhotoFacet accepted context item without an ID")
	}
	var contextID, title string
	if err := pool.QueryRow(ctx, `SELECT context_id,title FROM photo_contexts WHERE photo_id=$1`, photoID).Scan(&contextID, &title); err != nil || contextID != "set-1" || title != "original" {
		t.Fatalf("context after failed transaction = %q / %q, err %v", contextID, title, err)
	}
	var statusAt time.Time
	if err := pool.QueryRow(ctx, `SELECT photo_updated_at FROM photo_facet_status WHERE photo_id=$1 AND facet='contexts'`, photoID).Scan(&statusAt); err != nil || !statusAt.Equal(updatedAt) {
		t.Fatalf("facet status = %s, err %v; want %s", statusAt, err, updatedAt)
	}
	if err := repository.SetPhotoFacet(ctx, photoID, "info", []byte(`{"title":"seed"}`), updatedAt, fetchedAt); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT data FROM photo_info WHERE photo_id=$1`, photoID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var info map[string]string
	if err := json.Unmarshal(raw, &info); err != nil || info["title"] != "seed" {
		t.Fatalf("photo info=%s err=%v", raw, err)
	}
}
