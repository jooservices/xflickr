package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	aiapp "github.com/jooservices/xflickr/internal/ai/app"
	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

type captionWriterSpy struct {
	photoID int64
	model   string
	version string
	payload []byte
	calls   int
	err     error
}

func (f *captionWriterSpy) SaveCaption(_ context.Context, photoID int64, model, version string, payload []byte) error {
	f.calls++
	f.photoID = photoID
	f.model = model
	f.version = version
	f.payload = payload
	return f.err
}

type sequenceCaptioner struct {
	captions []aiapp.Caption
	calls    int
}

func (f *sequenceCaptioner) Describe(_ context.Context, _ []byte) (aiapp.Caption, error) {
	if f.calls >= len(f.captions) {
		return aiapp.Caption{}, errors.New("unexpected caption request")
	}
	caption := f.captions[f.calls]
	f.calls++
	return caption, nil
}

func TestStoreSavesCaptionJSON(t *testing.T) {
	writer := &captionWriterSpy{}
	store, err := NewStore(writer)
	if err != nil {
		t.Fatal(err)
	}
	caption := aiapp.Caption{Text: "A red boat", Objects: []string{"boat"}, Model: "vision", Version: "revision-7"}
	if err := store.SaveCaption(t.Context(), 42, caption); err != nil {
		t.Fatalf("SaveCaption() error = %v", err)
	}
	var got aiapp.Caption
	if err := json.Unmarshal(writer.payload, &got); err != nil {
		t.Fatalf("stored payload is invalid JSON: %v", err)
	}
	if writer.calls != 1 || writer.photoID != 42 || writer.model != caption.Model || writer.version != caption.Version || !reflect.DeepEqual(got, caption) {
		t.Fatalf("writer call = %+v, payload=%+v", writer, got)
	}
}

func TestStoreRequiresWriterAndWrapsErrors(t *testing.T) {
	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore() accepted a nil writer")
	}
	wantErr := errors.New("catalog write failed")
	store, err := NewStore(&captionWriterSpy{err: wantErr})
	if err != nil {
		t.Fatal(err)
	}
	caption := aiapp.Caption{Text: "A red boat", Model: "vision", Version: "revision-7"}
	if err := store.SaveCaption(t.Context(), 42, caption); !errors.Is(err, wantErr) {
		t.Fatalf("SaveCaption() error = %v, want writer error", err)
	}
	if err := (*Store)(nil).SaveCaption(t.Context(), 42, caption); err == nil {
		t.Fatal("nil Store accepted a write")
	}
}

func TestCaptionPhotoPersistsAndReplacesCaption(t *testing.T) {
	requireCaptionIntegration(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	pool := dbtest.Open(t, loadCaptionDatabase(t))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	suffix := fmt.Sprintf("ai-caption-%d", time.Now().UnixNano())
	photoID := time.Now().UnixNano()
	insertCaptionPhoto(t, ctx, tx, suffix, photoID)
	repository, err := postgres.New(tx)
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalogapp.NewCaptionService(repository)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(service)
	if err != nil {
		t.Fatal(err)
	}
	first := aiapp.Caption{Text: "A red boat", Objects: []string{"boat", "water"}, Model: "vision-a", Version: "revision-1"}
	replacement := aiapp.Caption{Text: "A blue boat", Objects: []string{"boat"}, Model: "vision-a", Version: "revision-2"}
	secondModel := aiapp.Caption{Text: "A blue boat by a pier", Objects: []string{"boat", "pier"}, Model: "vision-b", Version: "revision-3"}
	enricher, err := aiapp.NewEnricher(&sequenceCaptioner{captions: []aiapp.Caption{first, replacement, secondModel}}, store)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := enricher.CaptionPhoto(ctx, photoID, []byte{1, 2, 3}); err != nil {
			t.Fatalf("CaptionPhoto() error = %v", err)
		}
	}

	assertCaptionEnrichment(t, ctx, repository, photoID, first.Model, replacement)
	assertCaptionEnrichment(t, ctx, repository, photoID, secondModel.Model, secondModel)
}

func TestCaptionPersistencePlanUsesIndexes(t *testing.T) {
	requireCaptionIntegration(t)
	if os.Getenv("XFLICKR_PLAN_TEST") != "1" {
		t.Skip("set XFLICKR_PLAN_TEST=1 to build the 400,000-row transaction fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	t.Cleanup(cancel)
	pool := dbtest.Open(t, loadCaptionDatabase(t))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	const fixtureRows = 400000
	suffix := fmt.Sprintf("ai-caption-plan-%d", time.Now().UnixNano())
	basePhotoID := time.Now().UnixNano()
	if _, err := tx.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, $1)`, suffix); err != nil {
		t.Fatal(err)
	}
	photos, err := tx.Exec(ctx, `
		INSERT INTO photos (id, owner_nsid, last_update, visibility, raw, content_hash)
		SELECT $2::bigint + photo_id, $1, now(), 'public', '{}'::jsonb, decode(repeat('ab', 32), 'hex')
		FROM generate_series(0, $3 - 1) AS photo_id`, suffix, basePhotoID, fixtureRows)
	if err != nil || photos.RowsAffected() != fixtureRows {
		t.Fatalf("insert plan fixture photos: rows=%d error=%v", photos.RowsAffected(), err)
	}
	enrichments, err := tx.Exec(ctx, `
		INSERT INTO photo_enrichments (photo_id, kind, model, model_version, payload)
		SELECT $1::bigint + photo_id, 'caption', 'plan-model', 'revision-1', '{}'::jsonb
		FROM generate_series(0, $2 - 1) AS photo_id`, basePhotoID, fixtureRows)
	if err != nil || enrichments.RowsAffected() != fixtureRows {
		t.Fatalf("insert plan fixture enrichments: rows=%d error=%v", enrichments.RowsAffected(), err)
	}
	if _, err := tx.Exec(ctx, "ANALYZE photos"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "ANALYZE photo_enrichments"); err != nil {
		t.Fatal(err)
	}

	upsertPlan := decodePlan(t, explainPlan(t, ctx, tx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
		INSERT INTO photo_enrichments (photo_id, kind, model, model_version, payload)
		VALUES ($1, 'caption', 'plan-model', 'revision-2', '{"text":"updated"}')
		ON CONFLICT (photo_id, kind, model) DO UPDATE
		SET model_version = EXCLUDED.model_version, payload = EXCLUDED.payload`, basePhotoID))
	lookupPlan := decodePlan(t, explainPlan(t, ctx, tx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
		SELECT photo_id, kind, model, model_version, payload, created_at
		FROM photo_enrichments WHERE photo_id = $1 AND kind = $2 AND model = $3`, basePhotoID, "caption", "plan-model"))
	photoPlan := decodePlan(t, explainPlan(t, ctx, tx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
		SELECT id FROM photos WHERE id = $1`, basePhotoID))
	assertNoCatalogSequentialScans(t, upsertPlan)
	assertNoCatalogSequentialScans(t, lookupPlan)
	assertNoCatalogSequentialScans(t, photoPlan)
	if !planHasConflictArbiter(upsertPlan, "photo_enrichments_pkey") {
		t.Fatal("upsert plan does not use photo_enrichments_pkey as its conflict arbiter")
	}
	if !planHasIndex(lookupPlan, "photo_enrichments_pkey") {
		t.Fatal("lookup plan does not scan photo_enrichments_pkey")
	}
	if !planHasPhotoPrimaryKey(photoPlan) {
		t.Fatal("photo foreign-key lookup does not use a partition primary-key index")
	}
	t.Logf("upsert EXPLAIN (ANALYZE, BUFFERS): %s", upsertPlan)
	t.Logf("lookup EXPLAIN (ANALYZE, BUFFERS): %s", lookupPlan)
	t.Logf("photo foreign-key lookup EXPLAIN (ANALYZE, BUFFERS): %s", photoPlan)
}

func explainPlan(t *testing.T, ctx context.Context, tx pgx.Tx, query string, args ...any) []byte {
	t.Helper()
	var plan []byte
	if err := tx.QueryRow(ctx, query, args...).Scan(&plan); err != nil {
		t.Fatalf("read EXPLAIN plan: %v", err)
	}
	return plan
}

func decodePlan(t *testing.T, rawPlan []byte) any {
	t.Helper()
	var plan any
	if err := json.Unmarshal(rawPlan, &plan); err != nil {
		t.Fatalf("decode EXPLAIN JSON: %v", err)
	}
	return plan
}

func planContains(value any, predicate func(map[string]any) bool) bool {
	switch node := value.(type) {
	case map[string]any:
		if predicate(node) {
			return true
		}
		for _, child := range node {
			if planContains(child, predicate) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if planContains(child, predicate) {
				return true
			}
		}
	}
	return false
}

func assertNoCatalogSequentialScans(t *testing.T, plan any) {
	t.Helper()
	if planContains(plan, func(node map[string]any) bool {
		relation, _ := node["Relation Name"].(string)
		return node["Node Type"] == "Seq Scan" && (strings.HasPrefix(relation, "photos") || relation == "photo_enrichments")
	}) {
		t.Error("catalog persistence plan uses a sequential scan")
	}
}

func planHasConflictArbiter(plan any, want string) bool {
	return planContains(plan, func(node map[string]any) bool {
		indexes, _ := node["Conflict Arbiter Indexes"].([]any)
		for _, index := range indexes {
			if index == want {
				return true
			}
		}
		return false
	})
}

func planHasIndex(plan any, want string) bool {
	return planContains(plan, func(node map[string]any) bool { return node["Index Name"] == want })
}

func planHasPhotoPrimaryKey(plan any) bool {
	return planContains(plan, func(node map[string]any) bool {
		index, _ := node["Index Name"].(string)
		return index == "photos_pkey" || strings.HasPrefix(index, "photos_p") && strings.HasSuffix(index, "_pkey")
	})
}

func assertCaptionEnrichment(t *testing.T, ctx context.Context, repository *postgres.Repository, photoID int64, model string, want aiapp.Caption) {
	t.Helper()
	enrichment, found, err := repository.GetEnrichment(ctx, photoID, domain.EnrichmentCaption, model)
	if err != nil || !found {
		t.Fatalf("GetEnrichment(%q): found=%v error=%v", model, found, err)
	}
	var got aiapp.Caption
	if err := json.Unmarshal(enrichment.Payload, &got); err != nil {
		t.Fatalf("decode stored caption: %v", err)
	}
	if enrichment.PhotoID != photoID || enrichment.Kind != domain.EnrichmentCaption || enrichment.ModelVersion != want.Version || !reflect.DeepEqual(got, want) || enrichment.CreatedAt.IsZero() {
		t.Fatalf("stored enrichment = %+v payload=%+v, want model=%q caption=%+v", enrichment, got, model, want)
	}
}

func insertCaptionPhoto(t *testing.T, ctx context.Context, tx pgx.Tx, suffix string, photoID int64) {
	t.Helper()
	if _, err := tx.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, $1)`, suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO photos (id, owner_nsid, last_update, visibility, raw, content_hash)
		VALUES ($1, $2, now(), 'public', '{}'::jsonb, decode(repeat('ab', 32), 'hex'))`, photoID, suffix); err != nil {
		t.Fatal(err)
	}
}

func requireCaptionIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
}

func loadCaptionDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate AI caption integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load test database settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbConfig
}
