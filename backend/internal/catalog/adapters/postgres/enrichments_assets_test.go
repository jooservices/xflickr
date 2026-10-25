package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

func TestEnrichmentsAndAssetsRepositoryUnit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	errBoom := errors.New("database failed")
	jsonPayload := []byte(`{"caption":"test"}`)
	sha := make([]byte, 32)
	enrichment := domain.PhotoEnrichment{PhotoID: 17, Kind: domain.EnrichmentCaption, Model: "model", ModelVersion: "v1", Payload: jsonPayload}
	asset := domain.Asset{ID: 9, PhotoID: 17, SizeLabel: "Original", SHA256: sha, Bytes: 64, BlobKey: "photo/original", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	db := &catalogFeatureDB{}
	repository, err := postgres.New(db)
	if err != nil {
		t.Fatal(err)
	}

	if err := repository.UpsertEnrichment(ctx, enrichment); err != nil {
		t.Fatalf("UpsertEnrichment(): %v", err)
	}
	if err := repository.UpsertEnrichment(ctx, domain.PhotoEnrichment{PhotoID: 1, Kind: domain.EnrichmentCaption, Model: "m", ModelVersion: "v", Payload: []byte(`invalid`)}); err == nil {
		t.Fatal("UpsertEnrichment() accepted invalid JSON")
	}
	db.execErr = errBoom
	if err := repository.UpsertEnrichment(ctx, enrichment); !errors.Is(err, errBoom) {
		t.Fatalf("UpsertEnrichment() error = %v, want database error", err)
	}
	db.execErr = nil
	db.row = featureRow{values: []any{int64(17), domain.EnrichmentCaption, "model", "v1", jsonPayload, now}}
	if got, found, err := repository.GetEnrichment(ctx, 17, domain.EnrichmentCaption, "model"); err != nil || !found || got.ModelVersion != "v1" || string(got.Payload) != string(jsonPayload) {
		t.Fatalf("GetEnrichment() = %+v, %v, %v", got, found, err)
	}
	db.row = featureRow{err: pgx.ErrNoRows}
	if _, found, err := repository.GetEnrichment(ctx, 17, domain.EnrichmentCaption, "model"); err != nil || found {
		t.Fatalf("GetEnrichment() missing = found:%v err:%v", found, err)
	}
	db.row = featureRow{err: errBoom}
	if _, _, err := repository.GetEnrichment(ctx, 17, domain.EnrichmentCaption, "model"); !errors.Is(err, errBoom) {
		t.Fatalf("GetEnrichment() error = %v, want database error", err)
	}
	if _, _, err := repository.GetEnrichment(ctx, 17, "unknown", "model"); err == nil {
		t.Fatal("GetEnrichment() accepted an unknown kind")
	}
	if err := repository.DeleteEnrichment(ctx, 17, domain.EnrichmentCaption, "model"); err != nil {
		t.Fatalf("DeleteEnrichment(): %v", err)
	}
	db.execErr = errBoom
	if err := repository.DeleteEnrichment(ctx, 17, domain.EnrichmentCaption, "model"); !errors.Is(err, errBoom) {
		t.Fatalf("DeleteEnrichment() error = %v, want database error", err)
	}
	db.execErr = nil
	if err := repository.DeleteEnrichment(ctx, 17, "unknown", "model"); err == nil {
		t.Fatal("DeleteEnrichment() accepted an unknown kind")
	}

	db.row = featureRow{values: []any{int64(9), now}}
	created, err := repository.CreateAsset(ctx, domain.Asset{PhotoID: 17, SizeLabel: "Original", SHA256: sha, Bytes: 64, BlobKey: "photo/original", ExpiresAt: asset.ExpiresAt})
	if err != nil || created.ID != 9 || !created.CreatedAt.Equal(now) {
		t.Fatalf("CreateAsset() = %+v, %v", created, err)
	}
	if _, err := repository.CreateAsset(ctx, domain.Asset{PhotoID: 17}); err == nil {
		t.Fatal("CreateAsset() accepted invalid asset")
	}
	db.row = featureRow{err: errBoom}
	if _, err := repository.CreateAsset(ctx, domain.Asset{PhotoID: 17, SizeLabel: "Original", SHA256: sha, Bytes: 64, BlobKey: "key", ExpiresAt: asset.ExpiresAt}); !errors.Is(err, errBoom) {
		t.Fatalf("CreateAsset() error = %v, want database error", err)
	}
	db.row = featureRow{values: []any{int64(9), int64(17), "Original", sha, int64(64), "photo/original", asset.ExpiresAt, now}}
	if got, found, err := repository.GetAsset(ctx, 9); err != nil || !found || got.ID != 9 || got.Bytes != 64 {
		t.Fatalf("GetAsset() = %+v, %v, %v", got, found, err)
	}
	db.row = featureRow{err: pgx.ErrNoRows}
	if _, found, err := repository.GetAsset(ctx, 9); err != nil || found {
		t.Fatalf("GetAsset() missing = found:%v err:%v", found, err)
	}
	db.row = featureRow{err: errBoom}
	if _, _, err := repository.GetAsset(ctx, 9); !errors.Is(err, errBoom) {
		t.Fatalf("GetAsset() error = %v, want database error", err)
	}
	if _, _, err := repository.GetAsset(ctx, 0); err == nil {
		t.Fatal("GetAsset() accepted an invalid id")
	}
	if err := repository.UpdateAsset(ctx, asset); err != nil {
		t.Fatalf("UpdateAsset(): %v", err)
	}
	if err := repository.UpdateAsset(ctx, domain.Asset{ID: 9}); err == nil {
		t.Fatal("UpdateAsset() accepted invalid asset")
	}
	db.execErr = errBoom
	if err := repository.UpdateAsset(ctx, asset); !errors.Is(err, errBoom) {
		t.Fatalf("UpdateAsset() error = %v, want database error", err)
	}
	db.execErr = nil
	if err := repository.DeleteAsset(ctx, 9); err != nil {
		t.Fatalf("DeleteAsset(): %v", err)
	}
	if err := repository.DeleteAsset(ctx, 0); err == nil {
		t.Fatal("DeleteAsset() accepted an invalid id")
	}
	db.execErr = errBoom
	if err := repository.DeleteAsset(ctx, 9); !errors.Is(err, errBoom) {
		t.Fatalf("DeleteAsset() error = %v, want database error", err)
	}
	db.execErr = nil

	db.rows = &featureRows{values: [][]any{{int64(9), int64(17), "Original", sha, int64(64), "photo/original", now.Add(-time.Hour), now}}}
	if got, err := repository.ExpiredAssets(ctx, 10); err != nil || len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("ExpiredAssets() = %+v, %v", got, err)
	}
	if _, err := repository.ExpiredAssets(ctx, 0); err == nil {
		t.Fatal("ExpiredAssets() accepted a zero limit")
	}
	db.queryErr = errBoom
	if _, err := repository.ExpiredAssets(ctx, 10); !errors.Is(err, errBoom) {
		t.Fatalf("ExpiredAssets() query error = %v, want database error", err)
	}
	db.queryErr = nil
	db.rows = &featureRows{values: [][]any{{int64(9), int64(17), "Original", sha, int64(64), "photo/original", now, now}}, scanErr: errBoom}
	if _, err := repository.ExpiredAssets(ctx, 10); !errors.Is(err, errBoom) {
		t.Fatalf("ExpiredAssets() scan error = %v, want database error", err)
	}
	db.rows = &featureRows{rowsErr: errBoom}
	if _, err := repository.ExpiredAssets(ctx, 10); !errors.Is(err, errBoom) {
		t.Fatalf("ExpiredAssets() rows error = %v, want database error", err)
	}
}

type catalogFeatureDB struct {
	execErr  error
	queryErr error
	row      pgx.Row
	rows     pgx.Rows
}

func (db *catalogFeatureDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if db.execErr != nil {
		return pgconn.CommandTag{}, db.execErr
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (db *catalogFeatureDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if db.queryErr != nil {
		return nil, db.queryErr
	}
	return db.rows, nil
}

func (db *catalogFeatureDB) QueryRow(context.Context, string, ...any) pgx.Row       { return db.row }
func (db *catalogFeatureDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

type featureRow struct {
	values []any
	err    error
}

func (row featureRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return assignFeatureValues(dest, row.values)
}

type featureRows struct {
	pgx.Rows
	values  [][]any
	pos     int
	scanErr error
	rowsErr error
}

func (rows *featureRows) Next() bool {
	if rows.pos >= len(rows.values) {
		return false
	}
	rows.pos++
	return true
}
func (*featureRows) Close()          {}
func (rows *featureRows) Err() error { return rows.rowsErr }
func (rows *featureRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	return assignFeatureValues(dest, rows.values[rows.pos-1])
}

func assignFeatureValues(dest []any, values []any) error {
	if len(dest) != len(values) {
		return errors.New("unexpected scan value count")
	}
	for i := range dest {
		out := reflect.ValueOf(dest[i])
		value := reflect.ValueOf(values[i])
		if out.Kind() != reflect.Pointer || value.Type() != out.Elem().Type() {
			return errors.New("unexpected scan value type")
		}
		out.Elem().Set(value)
	}
	return nil
}
