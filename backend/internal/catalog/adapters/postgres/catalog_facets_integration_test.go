package postgres_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestCatalogFacetUpsertsAndKeysetPagination(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	repo, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("facet-%d", time.Now().UnixNano())
	person := suffix + "@example.invalid"
	contact := suffix + "-contact@example.invalid"
	albumID := suffix + "-album"
	galleryID := suffix + "-gallery"
	groupID := suffix + "-group"
	ids := []int64{time.Now().UnixNano(), time.Now().UnixNano() + 1, time.Now().UnixNano() + 2}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=ANY($1) OR contact_nsid=ANY($1)`, []string{person, contact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM person_groups WHERE person_nsid=$1`, person)
		_, _ = pool.Exec(context.Background(), `DELETE FROM groups WHERE nsid=$1`, groupID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM galleries WHERE id=$1`, galleryID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=$1`, albumID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1)`, ids)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{person, contact})
	}()
	if err := repo.UpsertPeople(ctx, []domain.Person{{NSID: person, Username: "facet-test"}, {NSID: contact, Username: "facet-contact"}}); err != nil {
		t.Fatal(err)
	}
	photos := make([]domain.Photo, 0, len(ids))
	for _, id := range ids {
		photos = append(photos, domain.Photo{ID: id, OwnerNSID: person, LastUpdate: time.Now().UTC(), Visibility: domain.VisibilityPublic, Raw: []byte(`{"id":1}`)})
	}
	if err := repo.UpsertPhotos(ctx, photos); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.UpsertAlbumsBatch(ctx, []postgres.Album{{ID: albumID, OwnerNSID: person, Title: "Album", PhotosCount: 3, UpdatedAt: now, SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertAlbumPhotosBatch(ctx, []postgres.AlbumPhoto{{AlbumID: albumID, PhotoID: ids[0], Position: 0, SeenAt: now}, {AlbumID: albumID, PhotoID: ids[1], Position: 1, SeenAt: now}, {AlbumID: albumID, PhotoID: ids[2], Position: 2, SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if err := repo.UpsertFavoritesBatch(ctx, []postgres.Favorite{{PersonNSID: person, PhotoID: id, FaveDate: now.Add(time.Duration(i) * time.Second), SeenAt: now}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.UpsertContactsBatch(ctx, []postgres.Contact{{PersonNSID: person, ContactNSID: contact, SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertGalleriesBatch(ctx, []postgres.Gallery{{ID: galleryID, OwnerNSID: person, Title: "Gallery", SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertGalleryPhotosBatch(ctx, []postgres.GalleryPhoto{{GalleryID: galleryID, PhotoID: ids[0], Position: 0, SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertGroupsBatch(ctx, []postgres.Group{{NSID: groupID, Name: "Group", SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertPersonGroupsBatch(ctx, []postgres.PersonGroup{{PersonNSID: person, GroupNSID: groupID, SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertGroupPoolPhotosBatch(ctx, []postgres.GroupPoolPhoto{{GroupNSID: groupID, PhotoID: ids[0], SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	// Repeating an identical batch must not rewrite rows.
	var before string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM favorites WHERE person_nsid=$1 AND photo_id=$2`, person, ids[2]).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertFavoritesBatch(ctx, []postgres.Favorite{{PersonNSID: person, PhotoID: ids[2], FaveDate: now.Add(2 * time.Second), SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM favorites WHERE person_nsid=$1 AND photo_id=$2`, person, ids[2]).Scan(&after); err != nil || before != after {
		t.Fatalf("favorite idempotency xmin %q -> %q, err=%v", before, after, err)
	}
	faves, err := repo.ListFavorites(ctx, person, nil, 2)
	if err != nil || len(faves.Items) != 2 || faves.NextCursor == nil || faves.Items[0].PhotoID != ids[2] {
		t.Fatalf("first favorites page=%+v, err=%v", faves, err)
	}
	faves2, err := repo.ListFavorites(ctx, person, faves.NextCursor, 2)
	if err != nil || len(faves2.Items) != 1 || faves2.Items[0].PhotoID != ids[0] {
		t.Fatalf("second favorites page=%+v, err=%v", faves2, err)
	}
	albums, err := repo.ListAlbums(ctx, person)
	if err != nil || len(albums) != 1 || albums[0].ID != albumID {
		t.Fatalf("albums=%+v, err=%v", albums, err)
	}
	page, err := repo.ListAlbumPhotos(ctx, albumID, nil, 2)
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil || page.Items[0].PhotoID != ids[0] {
		t.Fatalf("first album page=%+v, err=%v", page, err)
	}
	page2, err := repo.ListAlbumPhotos(ctx, albumID, page.NextCursor, 2)
	if err != nil || len(page2.Items) != 1 || page2.Items[0].PhotoID != ids[2] {
		t.Fatalf("second album page=%+v, err=%v", page2, err)
	}
	var partitioned int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_partitioned_table WHERE partrelid IN ('favorites'::regclass,'album_photos'::regclass)`).Scan(&partitioned); err != nil || partitioned != 2 {
		t.Fatalf("partitioned facet tables=%d, err=%v", partitioned, err)
	}
}
