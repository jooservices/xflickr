package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
)

type repositoryTestDB struct {
	batch      *repositoryTestBatch
	batchCalls int
	execErr    error
	execErrors []error
	execCalls  int
	queryErr   error
	rows       pgx.Rows
}

func (d *repositoryTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	index := d.execCalls
	d.execCalls++
	if index < len(d.execErrors) {
		return pgconn.CommandTag{}, d.execErrors[index]
	}
	return pgconn.CommandTag{}, d.execErr
}

func (d *repositoryTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return d.rows, d.queryErr
}
func (*repositoryTestDB) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (d *repositoryTestDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	d.batchCalls++
	return d.batch
}

type repositoryTestBatch struct {
	errs  []error
	index int
}

func (b *repositoryTestBatch) Exec() (pgconn.CommandTag, error) {
	index := b.index
	b.index++
	if index < len(b.errs) {
		return pgconn.CommandTag{}, b.errs[index]
	}
	return pgconn.CommandTag{}, nil
}

func (*repositoryTestBatch) Query() (pgx.Rows, error) { return nil, nil }
func (*repositoryTestBatch) QueryRow() pgx.Row        { return nil }
func (*repositoryTestBatch) Close() error             { return nil }

func TestRepositoryBatchWritesAndFailures(t *testing.T) {
	ctx := context.Background()
	db := &repositoryTestDB{batch: &repositoryTestBatch{}}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(ctx, []catalog.Person{{NSID: "owner", Username: "Owner"}}); err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	if err := repository.UpsertPhotos(ctx, []catalog.Photo{{
		ID: 1, OwnerNSID: "owner", LastUpdate: time.Unix(1700000000, 0),
		Visibility: catalog.VisibilityPublic, SizesStatus: catalog.SizesProvisional,
		Sizes: []byte(`[]`), Raw: []byte(`{"id":1}`),
	}}); err != nil {
		t.Fatalf("upsert photo: %v", err)
	}

	statementErr := errors.New("statement failed")
	db.execErr = statementErr
	if err := repository.UpsertPeople(ctx, []catalog.Person{{NSID: "owner", Username: "Owner"}}); !errors.Is(err, statementErr) {
		t.Fatalf("person batch error = %v", err)
	}
	db.execErr = nil
	batchErr := errors.New("batch write failed")
	db.batch = &repositoryTestBatch{errs: []error{batchErr}}
	if err := repository.UpsertPhotos(ctx, []catalog.Photo{{
		ID: 2, OwnerNSID: "owner", LastUpdate: time.Unix(1700000000, 0),
		Visibility: catalog.VisibilityPublic, Raw: []byte(`{"id":2}`),
	}}); !errors.Is(err, batchErr) {
		t.Fatalf("photo batch error = %v", err)
	}

	checkedAt := time.Unix(1700000100, 0)
	if err := repository.SetSizes(ctx, 1, []byte(`[]`), catalog.SizesConfirmed, checkedAt); err != nil {
		t.Fatalf("set sizes: %v", err)
	}
	if err := repository.MarkTombstone(ctx, 1, checkedAt); err != nil {
		t.Fatalf("mark tombstone: %v", err)
	}
	db.execErr = errors.New("statement failed")
	if err := repository.SetSizes(ctx, 1, []byte(`[]`), catalog.SizesConfirmed, checkedAt); !errors.Is(err, db.execErr) {
		t.Fatalf("set sizes statement error = %v", err)
	}
	if err := repository.MarkTombstone(ctx, 1, checkedAt); !errors.Is(err, db.execErr) {
		t.Fatalf("tombstone statement error = %v", err)
	}
}

func TestCrawlCatalogWritersMapDomainRecordsToBatchUpserts(t *testing.T) {
	db := &repositoryTestDB{batch: &repositoryTestBatch{}}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	photoID := int64(12)
	if err := repository.UpsertFavorites(ctx, []catalog.Favorite{{PersonNSID: "person", PhotoID: photoID, FaveDate: now}}); err != nil {
		t.Fatalf("upsert crawl favorites: %v", err)
	}
	if err := repository.UpsertAlbums(ctx, []catalog.Album{{ID: "album", OwnerNSID: "person", PrimaryPhotoID: &photoID, PhotosCount: 1}}); err != nil {
		t.Fatalf("upsert crawl albums: %v", err)
	}
	if err := repository.UpsertGalleries(ctx, []catalog.Gallery{{ID: "gallery", OwnerNSID: "person", PrimaryPhotoID: &photoID, PhotosCount: 1}}); err != nil {
		t.Fatalf("upsert crawl galleries: %v", err)
	}
	if err := repository.UpsertGroups(ctx, []catalog.Group{{NSID: "group", Name: "Group", MembersCount: 2, PoolCount: 3}}); err != nil {
		t.Fatalf("upsert crawl groups: %v", err)
	}
	if err := repository.UpsertPersonGroups(ctx, []catalog.PersonGroup{{PersonNSID: "person", GroupNSID: "group"}}); err != nil {
		t.Fatalf("upsert crawl person groups: %v", err)
	}
	if err := repository.UpsertContacts(ctx, []catalog.Contact{{PersonNSID: "person", ContactNSID: "contact"}}); err != nil {
		t.Fatalf("upsert crawl contacts: %v", err)
	}
	if err := repository.UpsertAlbumsBatch(ctx, []Album{{ID: "batch-album", OwnerNSID: "person", PrimaryPhotoID: &photoID, PhotosCount: 1}}); err != nil {
		t.Fatalf("upsert album batch: %v", err)
	}
	if err := repository.UpsertGalleriesBatch(ctx, []Gallery{{ID: "batch-gallery", OwnerNSID: "person", PrimaryPhotoID: &photoID, PhotosCount: 1}}); err != nil {
		t.Fatalf("upsert gallery batch: %v", err)
	}
	if db.batch.index != 9 {
		t.Fatalf("catalog batch statements = %d, want 9", db.batch.index)
	}
}

func TestPrepareFavorites(t *testing.T) {
	first := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	last := first.Add(time.Hour)
	input := []Favorite{
		{PersonNSID: "person-b", PhotoID: 2, FaveDate: first, SeenAt: first},
		{PersonNSID: "person-a", PhotoID: 1, FaveDate: first, SeenAt: first},
		{PersonNSID: "person-a", PhotoID: 1, FaveDate: last},
		{PersonNSID: "person-a", PhotoID: 2, FaveDate: first, SeenAt: first},
	}
	original := append([]Favorite(nil), input...)

	prepared, err := prepareFavorites(input)
	if err != nil {
		t.Fatalf("prepare favorites: %v", err)
	}
	wantKeys := []Favorite{
		{PersonNSID: "person-a", PhotoID: 1, FaveDate: last},
		{PersonNSID: "person-a", PhotoID: 2, FaveDate: first, SeenAt: first},
		{PersonNSID: "person-b", PhotoID: 2, FaveDate: first, SeenAt: first},
	}
	if len(prepared) != len(wantKeys) {
		t.Fatalf("prepared favorites = %d, want %d", len(prepared), len(wantKeys))
	}
	for i, want := range wantKeys {
		got := prepared[i]
		if got.PersonNSID != want.PersonNSID || got.PhotoID != want.PhotoID || !got.FaveDate.Equal(want.FaveDate) {
			t.Fatalf("prepared[%d] = %+v, want key/date %+v", i, got, want)
		}
		if want.SeenAt.IsZero() {
			if got.SeenAt.IsZero() {
				t.Fatalf("prepared[%d] has zero SeenAt", i)
			}
		} else if !got.SeenAt.Equal(want.SeenAt) {
			t.Fatalf("prepared[%d].SeenAt = %s, want %s", i, got.SeenAt, want.SeenAt)
		}
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("prepareFavorites modified input: got %+v, want %+v", input, original)
	}

	invalidCases := []struct {
		name     string
		favorite Favorite
	}{
		{name: "missing person", favorite: Favorite{PhotoID: 1, FaveDate: first}},
		{name: "nonpositive photo", favorite: Favorite{PersonNSID: "person-a", FaveDate: first}},
		{name: "missing date", favorite: Favorite{PersonNSID: "person-a", PhotoID: 1}},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := prepareFavorites([]Favorite{tc.favorite}); err == nil {
				t.Fatal("invalid favorite succeeded")
			}
		})
	}
}

func TestFavoritesBatchRejectsInvalidBeforeWrite(t *testing.T) {
	valid := Favorite{PersonNSID: "person", PhotoID: 1, FaveDate: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)}
	tooMany := make([]Favorite, favoritesBatchMax+1)
	for i := range tooMany {
		tooMany[i] = valid
		tooMany[i].PhotoID = int64(i + 1)
	}
	maximum := append([]Favorite(nil), tooMany[:favoritesBatchMax]...)
	cases := []struct {
		name           string
		favorites      []Favorite
		wantError      bool
		wantBatch      int
		wantStatements int
	}{
		{name: "empty"},
		{name: "invalid after valid", favorites: []Favorite{valid, {PersonNSID: "person", PhotoID: 2}}, wantError: true},
		{name: "over maximum", favorites: tooMany, wantError: true},
		{name: "maximum", favorites: maximum, wantBatch: 1, wantStatements: favoritesBatchMax * 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := &repositoryTestDB{batch: &repositoryTestBatch{}}
			repository, err := New(db)
			if err != nil {
				t.Fatal(err)
			}
			err = repository.UpsertFavoritesBatch(t.Context(), tc.favorites)
			if (err != nil) != tc.wantError {
				t.Fatalf("UpsertFavoritesBatch error = %v, wantError %t", err, tc.wantError)
			}
			if db.batchCalls != tc.wantBatch || db.batch.index != tc.wantStatements {
				t.Fatalf("SendBatch calls=%d statements=%d, want %d calls and %d statements", db.batchCalls, db.batch.index, tc.wantBatch, tc.wantStatements)
			}
		})
	}
}

func TestFavoritesBatchPropagatesErrors(t *testing.T) {
	batchError := errors.New("favorite batch failed")
	favorite := Favorite{PersonNSID: "person", PhotoID: 1, FaveDate: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)}
	cases := []struct {
		name      string
		errs      []error
		wantExecs int
	}{
		{name: "insert phase", errs: []error{batchError}, wantExecs: 1},
		{name: "update phase", errs: []error{nil, batchError}, wantExecs: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := &repositoryTestDB{batch: &repositoryTestBatch{errs: tc.errs}}
			repository, err := New(db)
			if err != nil {
				t.Fatal(err)
			}
			err = repository.UpsertFavoritesBatch(t.Context(), []Favorite{favorite})
			if !errors.Is(err, batchError) {
				t.Fatalf("UpsertFavoritesBatch error = %v, want wrapped batch error", err)
			}
			if db.batch.index != tc.wantExecs {
				t.Fatalf("batch statements executed = %d, want %d", db.batch.index, tc.wantExecs)
			}
			if db.batchCalls != 1 {
				t.Fatalf("SendBatch calls = %d, want 1", db.batchCalls)
			}
		})
	}
}

func TestRepositoryValidatesBeforeWriting(t *testing.T) {
	repository, err := New(&repositoryTestDB{batch: &repositoryTestBatch{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := repository.UpsertPeople(ctx, nil); err != nil {
		t.Fatalf("empty people batch: %v", err)
	}
	if err := repository.UpsertPhotos(ctx, nil); err != nil {
		t.Fatalf("empty photo batch: %v", err)
	}
	if err := repository.UpsertPeople(ctx, []catalog.Person{{NSID: "owner"}}); err == nil {
		t.Fatal("person without username succeeded")
	}
	if err := repository.UpsertPhotos(ctx, []catalog.Photo{{ID: 1, OwnerNSID: "owner", LastUpdate: time.Now(), Visibility: "invalid", Raw: []byte(`{}`)}}); err == nil {
		t.Fatal("invalid photo succeeded")
	}
	if err := repository.SetSizes(ctx, 0, []byte(`[]`), catalog.SizesConfirmed, time.Now()); err == nil {
		t.Fatal("zero photo id succeeded")
	}
	if err := repository.SetSizes(ctx, 1, []byte(`bad`), catalog.SizesConfirmed, time.Now()); err == nil {
		t.Fatal("invalid sizes JSON succeeded")
	}
	if err := repository.MarkTombstone(ctx, 0, time.Now()); err == nil {
		t.Fatal("zero photo id tombstone succeeded")
	}
}
