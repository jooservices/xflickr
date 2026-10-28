package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestCatalogFacetBatchWritersValidateAndWrite(t *testing.T) {
	ctx := context.Background()
	db := &repositoryTestDB{batch: &repositoryTestBatch{}}
	repo, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	photoID := int64(22)
	cases := []struct {
		name  string
		write func() error
		bad   func() error
	}{
		{"albums", func() error {
			return repo.UpsertAlbumsBatch(ctx, []Album{{ID: "a", OwnerNSID: "p", PrimaryPhotoID: &photoID, PhotosCount: 1}})
		}, func() error { return repo.UpsertAlbumsBatch(ctx, []Album{{ID: "a", OwnerNSID: "p", PhotosCount: -1}}) }},
		{"album photos", func() error {
			return repo.UpsertAlbumPhotosBatch(ctx, []AlbumPhoto{{AlbumID: "a", PhotoID: photoID, Position: 1, SeenAt: now}})
		}, func() error {
			return repo.UpsertAlbumPhotosBatch(ctx, []AlbumPhoto{{AlbumID: "a", PhotoID: photoID, Position: -1}})
		}},
		{"contacts", func() error { return repo.UpsertContactsBatch(ctx, []Contact{{PersonNSID: "p", ContactNSID: "c"}}) }, func() error { return repo.UpsertContactsBatch(ctx, []Contact{{PersonNSID: "p", ContactNSID: "p"}}) }},
		{"favorites", func() error {
			return repo.UpsertFavoritesBatch(ctx, []Favorite{{PersonNSID: "p", PhotoID: photoID, FaveDate: now}})
		}, func() error { return repo.UpsertFavoritesBatch(ctx, []Favorite{{PersonNSID: "p", PhotoID: photoID}}) }},
		{"galleries", func() error {
			return repo.UpsertGalleriesBatch(ctx, []Gallery{{ID: "g", OwnerNSID: "p", PhotosCount: 1}})
		}, func() error {
			return repo.UpsertGalleriesBatch(ctx, []Gallery{{ID: "g", OwnerNSID: "p", PhotosCount: -1}})
		}},
		{"gallery photos", func() error {
			return repo.UpsertGalleryPhotosBatch(ctx, []GalleryPhoto{{GalleryID: "g", PhotoID: photoID}})
		}, func() error { return repo.UpsertGalleryPhotosBatch(ctx, []GalleryPhoto{{GalleryID: "g", PhotoID: 0}}) }},
		{"groups", func() error {
			return repo.UpsertGroupsBatch(ctx, []Group{{NSID: "g", Name: "Group", MembersCount: 1, PoolCount: 2}})
		}, func() error { return repo.UpsertGroupsBatch(ctx, []Group{{NSID: "g", Name: "Group", PoolCount: -1}}) }},
		{"person groups", func() error {
			return repo.UpsertPersonGroupsBatch(ctx, []PersonGroup{{PersonNSID: "p", GroupNSID: "g"}})
		}, func() error { return repo.UpsertPersonGroupsBatch(ctx, []PersonGroup{{PersonNSID: "p"}}) }},
		{"group pool photos", func() error {
			return repo.UpsertGroupPoolPhotosBatch(ctx, []GroupPoolPhoto{{GroupNSID: "g", PhotoID: photoID}})
		}, func() error { return repo.UpsertGroupPoolPhotosBatch(ctx, []GroupPoolPhoto{{GroupNSID: "g"}}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db.batch = &repositoryTestBatch{}
			if err := tc.write(); err != nil {
				t.Fatalf("valid write: %v", err)
			}
			if db.batch.index != 1 {
				t.Fatalf("executed %d batch statements", db.batch.index)
			}
			if err := tc.bad(); err == nil {
				t.Fatal("invalid record was accepted")
			}
		})
	}
	if err := repo.UpsertContactsBatch(ctx, nil); err != nil {
		t.Fatalf("empty contacts: %v", err)
	}
	if err := repo.UpsertGroupPoolPhotosBatch(ctx, make([]GroupPoolPhoto, UpsertPeopleBatchMax+1)); err == nil {
		t.Fatal("oversized batch was accepted")
	}
}

func TestListAlbumsAndPhotosHandlePagesAndFailures(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	primary := int64(22)
	db := &repositoryTestDB{}
	repo, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	db.rows = &facetRows{rows: [][]any{
		{"a", "owner", nil, "A", "", int64(0), (*time.Time)(nil), (*time.Time)(nil), now},
		{"b", "owner", &primary, "B", "desc", int64(2), &now, &now, now},
	}}
	albums, err := repo.ListAlbums(ctx, "owner")
	if err != nil || len(albums) != 2 || albums[0].PrimaryPhotoID != nil || !albums[0].CreatedAt.IsZero() || albums[1].PrimaryPhotoID == nil || albums[1].CreatedAt != now {
		t.Fatalf("albums=%+v err=%v", albums, err)
	}
	db.rows = &facetRows{rows: [][]any{{"a", int64(22), 1, now}, {"a", int64(23), 2, now}}}
	page, err := repo.ListAlbumPhotos(ctx, "a", nil, 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.NextCursor.PhotoID != 22 {
		t.Fatalf("album page=%+v err=%v", page, err)
	}
	db.rows = &facetRows{rows: [][]any{{"a", int64(24), 3, now}}}
	page, err = repo.ListAlbumPhotos(ctx, "a", &AlbumPhotoCursor{Position: 2, PhotoID: 23}, 2)
	if err != nil || len(page.Items) != 1 || page.NextCursor != nil {
		t.Fatalf("album cursor page=%+v err=%v", page, err)
	}
	db.rows = &facetRows{rows: [][]any{{"owner", int64(22), now, now}, {"owner", int64(23), now.Add(-time.Hour), now}}}
	favorites, err := repo.ListFavorites(ctx, "owner", nil, 1)
	if err != nil || len(favorites.Items) != 1 || favorites.NextCursor == nil || favorites.NextCursor.PhotoID != 22 {
		t.Fatalf("favorite page=%+v err=%v", favorites, err)
	}
	db.rows = &facetRows{rows: [][]any{{"owner", int64(24), now.Add(-2 * time.Hour), now}}}
	favorites, err = repo.ListFavorites(ctx, "owner", &FavoriteCursor{FaveDate: now, PhotoID: 23}, 2)
	if err != nil || len(favorites.Items) != 1 || favorites.NextCursor != nil {
		t.Fatalf("favorite cursor page=%+v err=%v", favorites, err)
	}
	if _, err := repo.ListAlbums(ctx, ""); err == nil {
		t.Fatal("empty person was accepted")
	}
	if _, err := repo.ListAlbumPhotos(ctx, "a", nil, 0); err == nil {
		t.Fatal("invalid album limit was accepted")
	}
	if _, err := repo.ListAlbumPhotos(ctx, "a", &AlbumPhotoCursor{Position: -1, PhotoID: 2}, 1); err == nil {
		t.Fatal("invalid album cursor was accepted")
	}
	if _, err := repo.ListFavorites(ctx, "", nil, 1); err == nil {
		t.Fatal("empty favorite person was accepted")
	}
	if _, err := repo.ListFavorites(ctx, "owner", &FavoriteCursor{PhotoID: 1}, 1); err == nil {
		t.Fatal("invalid favorite cursor was accepted")
	}

	boom := errors.New("query failed")
	db.queryErr = boom
	if _, err := repo.ListAlbums(ctx, "owner"); !errors.Is(err, boom) {
		t.Fatalf("album query error=%v", err)
	}
	if _, err := repo.ListAlbumPhotos(ctx, "a", nil, 1); !errors.Is(err, boom) {
		t.Fatalf("album photos query error=%v", err)
	}
	if _, err := repo.ListFavorites(ctx, "owner", nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorites query error=%v", err)
	}
	db.queryErr = nil
	db.rows = &facetRows{rows: [][]any{{"a", "owner", nil, "A", "", int64(0), (*time.Time)(nil), (*time.Time)(nil), now}}, scanErr: boom}
	if _, err := repo.ListAlbums(ctx, "owner"); !errors.Is(err, boom) {
		t.Fatalf("album scan error=%v", err)
	}
	db.rows = &facetRows{rows: [][]any{{"a", int64(22), 1, now}}, scanErr: boom}
	if _, err := repo.ListAlbumPhotos(ctx, "a", nil, 1); !errors.Is(err, boom) {
		t.Fatalf("album photo scan error=%v", err)
	}
	db.rows = &facetRows{rows: [][]any{{"owner", int64(22), now, now}}, scanErr: boom}
	if _, err := repo.ListFavorites(ctx, "owner", nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorite scan error=%v", err)
	}
	db.rows = &facetRows{err: boom}
	if _, err := repo.ListAlbums(ctx, "owner"); !errors.Is(err, boom) {
		t.Fatalf("album rows error=%v", err)
	}
	db.rows = &facetRows{err: boom}
	if _, err := repo.ListAlbumPhotos(ctx, "a", nil, 1); !errors.Is(err, boom) {
		t.Fatalf("album photo rows error=%v", err)
	}
	db.rows = &facetRows{err: boom}
	if _, err := repo.ListFavorites(ctx, "owner", nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorite rows error=%v", err)
	}
}

type facetRows struct {
	pgx.Rows
	rows    [][]any
	index   int
	scanErr error
	err     error
}

func (rows *facetRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}
func (*facetRows) Close()          {}
func (rows *facetRows) Err() error { return rows.err }
func (rows *facetRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	values := rows.rows[rows.index-1]
	if len(dest) != len(values) {
		return errors.New("unexpected scan value count")
	}
	for i, target := range dest {
		out := reflect.ValueOf(target)
		if out.Kind() != reflect.Pointer {
			return errors.New("scan destination is not a pointer")
		}
		if values[i] == nil {
			out.Elem().SetZero()
			continue
		}
		value := reflect.ValueOf(values[i])
		if !value.Type().AssignableTo(out.Elem().Type()) {
			return errors.New("unexpected scan value type")
		}
		out.Elem().Set(value)
	}
	return nil
}
