package postgres_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestCatalogUpsertsBatchIdempotencySizesAndPartitions(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dbConfig := loadCatalogDatabase(t)
	pool := dbtest.Open(t, dbConfig)
	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("catalog-%d", time.Now().UnixNano())
	nsid := suffix + "@example.invalid"
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid = $1`, nsid)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid = $1`, nsid)
	}()

	people := make([]domain.Person, 500)
	photos := make([]domain.Photo, 500)
	lastUpdate := time.Now().UTC().Truncate(time.Microsecond)
	for i := range people {
		people[i] = domain.Person{NSID: fmt.Sprintf("%s-%d", nsid, i), Username: fmt.Sprintf("person-%d", i)}
		photos[i] = domain.Photo{
			ID: time.Now().UnixNano() + int64(i), OwnerNSID: nsid, LastUpdate: lastUpdate,
			Visibility: domain.VisibilityPublic, Raw: []byte(fmt.Sprintf(`{"photo":%d}`, i)),
		}
	}
	// Photo ownership points to one person; store that owner plus the other
	// people records in the same 500-row batch.
	people[0].NSID = nsid
	if err := repository.UpsertPeople(ctx, people); err != nil {
		t.Fatalf("UpsertPeople(500): %v", err)
	}
	if err := repository.UpsertPhotos(ctx, photos); err != nil {
		t.Fatalf("UpsertPhotos(500): %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM photos WHERE owner_nsid = $1`, nsid).Scan(&count); err != nil || count != 500 {
		t.Fatalf("photo count = %d, error = %v; want 500", count, err)
	}
	var personXmin string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM people WHERE nsid = $1`, nsid).Scan(&personXmin); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(ctx, []domain.Person{people[0]}); err != nil {
		t.Fatalf("repeat person upsert: %v", err)
	}
	var unchangedPersonXmin string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM people WHERE nsid = $1`, nsid).Scan(&unchangedPersonXmin); err != nil {
		t.Fatal(err)
	}
	if unchangedPersonXmin != personXmin {
		t.Fatalf("unchanged person wrote a row: xmin %s -> %s", personXmin, unchangedPersonXmin)
	}

	photoID := photos[0].ID
	var originalXmin string
	var originalSeenAt time.Time
	if err := pool.QueryRow(ctx, `SELECT xmin::text,last_seen_at FROM photos WHERE id = $1`, photoID).Scan(&originalXmin, &originalSeenAt); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := repository.UpsertPhotos(ctx, []domain.Photo{photos[0]}); err != nil {
		t.Fatalf("repeat photo upsert: %v", err)
	}
	var unchangedXmin string
	var refreshedSeenAt time.Time
	if err := pool.QueryRow(ctx, `SELECT xmin::text,last_seen_at FROM photos WHERE id = $1`, photoID).Scan(&unchangedXmin, &refreshedSeenAt); err != nil {
		t.Fatal(err)
	}
	if unchangedXmin == originalXmin || !refreshedSeenAt.After(originalSeenAt) {
		t.Fatalf("photo observation was not refreshed: xmin %s -> %s, last_seen_at %s -> %s", originalXmin, unchangedXmin, originalSeenAt, refreshedSeenAt)
	}
	var compressed []byte
	if err := pool.QueryRow(ctx, `SELECT raw FROM photos WHERE id = $1`, photoID).Scan(&compressed); err != nil {
		t.Fatal(err)
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	var rawEnvelope struct {
		Encoding string `json:"encoding"`
		Data     string `json:"data"`
	}
	if err := json.Unmarshal(compressed, &rawEnvelope); err != nil {
		t.Fatal(err)
	}
	if rawEnvelope.Encoding != "zstd" {
		t.Fatalf("raw encoding = %q, want zstd", rawEnvelope.Encoding)
	}
	compressedBytes, err := base64.StdEncoding.DecodeString(rawEnvelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decoder.DecodeAll(compressedBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(photos[0].Raw) {
		t.Fatalf("decompressed raw = %s, want %s", got, photos[0].Raw)
	}

	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := repository.SetSizes(ctx, photoID, []byte(`[{"label":"Original"}]`), domain.SizesProvisional, checkedAt); err != nil {
		t.Fatalf("set provisional sizes: %v", err)
	}
	confirmedAt := checkedAt.Add(time.Second)
	confirmed := []byte(`[{"label":"Original","source":"confirmed"}]`)
	if err := repository.SetSizes(ctx, photoID, confirmed, domain.SizesConfirmed, confirmedAt); err != nil {
		t.Fatalf("set confirmed sizes: %v", err)
	}
	var confirmedXmin string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM photos WHERE id = $1`, photoID).Scan(&confirmedXmin); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetSizes(ctx, photoID, confirmed, domain.SizesConfirmed, confirmedAt); err != nil {
		t.Fatalf("repeat confirmed sizes: %v", err)
	}
	var unchangedSizesXmin string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM photos WHERE id = $1`, photoID).Scan(&unchangedSizesXmin); err != nil {
		t.Fatal(err)
	}
	if unchangedSizesXmin != confirmedXmin {
		t.Fatalf("unchanged sizes wrote a row: xmin %s -> %s", confirmedXmin, unchangedSizesXmin)
	}
	if err := repository.SetSizes(ctx, photoID, []byte(`[]`), domain.SizesProvisional, confirmedAt.Add(time.Second)); err != nil {
		t.Fatalf("set stale provisional sizes: %v", err)
	}
	var storedSizes []byte
	var storedStatus string
	var storedCheckedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT sizes, sizes_status, sizes_checked_at FROM photos WHERE id = $1`, photoID).Scan(&storedSizes, &storedStatus, &storedCheckedAt); err != nil {
		t.Fatal(err)
	}
	var storedJSON, confirmedJSON any
	if err := json.Unmarshal(storedSizes, &storedJSON); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(confirmed, &confirmedJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(storedJSON, confirmedJSON) || storedStatus != string(domain.SizesConfirmed) || !storedCheckedAt.Equal(confirmedAt) {
		t.Fatalf("stale sizes downgraded confirmed record: sizes=%s status=%s checked=%s", storedSizes, storedStatus, storedCheckedAt)
	}
	unavailableAt := confirmedAt.Add(2 * time.Second)
	if err := repository.SetSizes(ctx, photoID, []byte(`[]`), domain.SizesUnavailable, unavailableAt); err != nil {
		t.Fatalf("mark confirmed sizes unavailable: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT sizes, sizes_status, sizes_checked_at FROM photos WHERE id = $1`, photoID).Scan(&storedSizes, &storedStatus, &storedCheckedAt); err != nil {
		t.Fatal(err)
	}
	if string(storedSizes) != `[]` || storedStatus != string(domain.SizesUnavailable) || !storedCheckedAt.Equal(unavailableAt) {
		t.Fatalf("confirmed to unavailable update failed: sizes=%s status=%s checked=%s", storedSizes, storedStatus, storedCheckedAt)
	}

	tombstoneAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := repository.MarkTombstone(ctx, photoID, tombstoneAt); err != nil {
		t.Fatalf("mark tombstone: %v", err)
	}
	var tombstoneXmin string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM photos WHERE id = $1`, photoID).Scan(&tombstoneXmin); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkTombstone(ctx, photoID, tombstoneAt.Add(time.Second)); err != nil {
		t.Fatalf("repeat tombstone: %v", err)
	}
	var storedTombstone time.Time
	var unchangedTombstoneXmin string
	if err := pool.QueryRow(ctx, `SELECT tombstoned_at, xmin::text FROM photos WHERE id = $1`, photoID).Scan(&storedTombstone, &unchangedTombstoneXmin); err != nil || !storedTombstone.Equal(tombstoneAt) || unchangedTombstoneXmin != tombstoneXmin {
		t.Fatalf("tombstone = %s xmin=%s, error = %v; want %s xmin=%s", storedTombstone, unchangedTombstoneXmin, err, tombstoneAt, tombstoneXmin)
	}
	if err := repository.UpsertPhotos(ctx, []domain.Photo{photos[0]}); err != nil {
		t.Fatalf("upsert photo found again after tombstoning: %v", err)
	}
	var revivedTombstone *time.Time
	if err := pool.QueryRow(ctx, `SELECT tombstoned_at FROM photos WHERE id=$1`, photoID).Scan(&revivedTombstone); err != nil || revivedTombstone != nil {
		t.Fatalf("photo returned by a new scan retained its tombstone: tombstoned_at=%v err=%v", revivedTombstone, err)
	}

	var partitionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_inherits WHERE inhparent = 'photos'::regclass`).Scan(&partitionCount); err != nil || partitionCount != 16 {
		t.Fatalf("photos partition count = %d, error = %v; want 16", partitionCount, err)
	}
	var rawType string
	if err := pool.QueryRow(ctx, `
		SELECT format_type(attribute.atttypid, attribute.atttypmod)
		FROM pg_attribute AS attribute
		WHERE attribute.attrelid = 'photos'::regclass AND attribute.attname = 'raw'
	`).Scan(&rawType); err != nil || rawType != "jsonb" {
		t.Fatalf("photos.raw type = %q, error = %v; want jsonb", rawType, err)
	}
}

func loadCatalogDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate catalog integration test source")
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
