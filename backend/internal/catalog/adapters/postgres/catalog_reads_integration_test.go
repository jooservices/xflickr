package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestCatalogReadsApplyTwoUserVisibilityPolicy(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	reads := app.ReadService{Repository: repository}
	suffix := fmt.Sprintf("catalog-read-%d", time.Now().UnixNano())
	user1NSID, user2NSID, hiddenNSID := suffix+"-u1", suffix+"-u2", suffix+"-hidden"
	personIDs := []string{user1NSID, user2NSID, hiddenNSID}
	photoIDs := []int64{time.Now().UnixNano(), time.Now().UnixNano() + 1, time.Now().UnixNano() + 2}
	albumID, hiddenAlbumID := suffix+"-album", suffix+"-hidden-album"
	var user1ID, user2ID, account1ID, account2ID int64
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=ANY($1)`, []string{albumID, hiddenAlbumID})
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1)`, photoIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, personIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=ANY($1)`, []int64{user1ID, user2ID})
	}()
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-u1@example.invalid").Scan(&user1ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-u2@example.invalid").Scan(&user2ID); err != nil {
		t.Fatal(err)
	}
	var key1ID, key2ID int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'test',$2,'{}',1000,10,20) RETURNING id`, user1ID, suffix+"-key-1").Scan(&key1ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'test',$2,'{}',1000,10,20) RETURNING id`, user2ID, suffix+"-key-2").Scan(&key2ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'user-one','{}','{}','ok') RETURNING id`, user1ID, key1ID, user1NSID).Scan(&account1ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'user-two','{}','{}','ok') RETURNING id`, user2ID, key2ID, user2NSID).Scan(&account2ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, user2ID); err != nil {
		t.Fatal(err)
	}
	people := []domain.Person{{NSID: user1NSID, Username: "one"}, {NSID: user2NSID, Username: "two"}, {NSID: hiddenNSID, Username: "hidden"}}
	if err := repository.UpsertPeople(ctx, people); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	photos := []domain.Photo{
		{ID: photoIDs[0], OwnerNSID: user1NSID, Title: "private to one", LastUpdate: now, Visibility: domain.VisibilityPrivate, Raw: []byte(`{"id":1}`)},
		{ID: photoIDs[1], OwnerNSID: user2NSID, Title: "shared public", LastUpdate: now.Add(-time.Second), Visibility: domain.VisibilityPublic, Tags: []string{"shared"}, Sizes: []byte(`[{"label":"Large","width":1200,"height":800}]`), SizesStatus: domain.SizesConfirmed, Raw: []byte(`{"id":2}`)},
		{ID: photoIDs[2], OwnerNSID: hiddenNSID, Title: "private to two", LastUpdate: now.Add(-2 * time.Second), Visibility: domain.VisibilityPrivate, Raw: []byte(`{"id":3}`)},
	}
	if err := repository.UpsertPhotos(ctx, photos); err != nil {
		t.Fatal(err)
	}
	observations := []struct {
		userID, accountID int64
		kind, id          string
	}{
		{user1ID, account1ID, "person", user1NSID},
		{user1ID, account1ID, "photo", fmt.Sprint(photoIDs[0])},
		{user2ID, account2ID, "person", user2NSID},
		{user2ID, account2ID, "person", hiddenNSID},
		{user2ID, account2ID, "photo", fmt.Sprint(photoIDs[1])},
		{user2ID, account2ID, "photo", fmt.Sprint(photoIDs[2])},
	}
	for _, observation := range observations {
		if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,$3,$4)`, observation.userID, observation.accountID, observation.kind, observation.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.UpsertFavoritesBatch(ctx, []postgres.Favorite{
		{PersonNSID: user1NSID, PhotoID: photoIDs[1], FaveDate: now},
		{PersonNSID: user1NSID, PhotoID: photoIDs[2], FaveDate: now.Add(-time.Second)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertAlbumsBatch(ctx, []postgres.Album{
		{ID: albumID, OwnerNSID: user1NSID, Title: "Mixed", PrimaryPhotoID: &photoIDs[2], PhotosCount: 2, UpdatedAt: now},
		{ID: hiddenAlbumID, OwnerNSID: user1NSID, Title: "Hidden", PrimaryPhotoID: &photoIDs[2], PhotosCount: 1, UpdatedAt: now.Add(-time.Second)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertAlbumPhotosBatch(ctx, []postgres.AlbumPhoto{
		{AlbumID: albumID, PhotoID: photoIDs[1], Position: 0},
		{AlbumID: albumID, PhotoID: photoIDs[2], Position: 1},
		{AlbumID: hiddenAlbumID, PhotoID: photoIDs[2], Position: 0},
	}); err != nil {
		t.Fatal(err)
	}

	peoplePage, next, err := reads.ListPeople(ctx, user1ID, "", 1)
	if err != nil || len(peoplePage.Items) != 1 || next == "" {
		t.Fatalf("first people page=%+v next=%q err=%v", peoplePage, next, err)
	}
	peoplePage2, _, err := reads.ListPeople(ctx, user1ID, next, 5)
	if err != nil || len(peoplePage2.Items) != 1 {
		t.Fatalf("second people page=%+v err=%v", peoplePage2, err)
	}
	visiblePeople := map[string]int64{peoplePage.Items[0].NSID: peoplePage.Items[0].PhotosCount, peoplePage2.Items[0].NSID: peoplePage2.Items[0].PhotosCount}
	if len(visiblePeople) != 2 || visiblePeople[user1NSID] != 1 || visiblePeople[user2NSID] != 1 {
		t.Fatalf("user one visible people/counts=%v", visiblePeople)
	}
	if _, err := reads.GetPerson(ctx, user1ID, hiddenNSID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("hidden person error=%v", err)
	}
	personPhotos, _, err := reads.ListPersonPhotos(ctx, user1ID, user2NSID, "", 10)
	if err != nil || len(personPhotos.Items) != 1 || personPhotos.Items[0].ID != photoIDs[1] || personPhotos.Items[0].SizesStatus != "confirmed" || personPhotos.Items[0].Sizes[0].Label != "Large" {
		t.Fatalf("visible person photos=%+v err=%v", personPhotos, err)
	}
	if _, err := reads.GetPhoto(ctx, user2ID, photoIDs[0]); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("photo hidden from user two error=%v", err)
	}
	if photo, err := reads.GetPhoto(ctx, user1ID, photoIDs[1]); err != nil || photo.ID != photoIDs[1] {
		t.Fatalf("shared photo=%+v err=%v", photo, err)
	}
	favorites, _, err := reads.ListFavorites(ctx, user1ID, "", 10)
	if err != nil || len(favorites.Items) != 1 || favorites.Items[0].Photo.ID != photoIDs[1] {
		t.Fatalf("visible favorites=%+v err=%v", favorites, err)
	}
	albums, _, err := reads.ListAlbums(ctx, user1ID, "", 10)
	if err != nil || len(albums.Items) != 1 || albums.Items[0].ID != albumID || albums.Items[0].VisiblePhotosCount != 1 || albums.Items[0].PrimaryPhotoID != nil {
		t.Fatalf("visible albums=%+v err=%v", albums, err)
	}
	albumPhotos, _, err := reads.ListAlbumPhotos(ctx, user1ID, albumID, "", 10)
	if err != nil || len(albumPhotos.Items) != 1 || albumPhotos.Items[0].ID != photoIDs[1] {
		t.Fatalf("visible album photos=%+v err=%v", albumPhotos, err)
	}
	if _, _, err := reads.ListAlbumPhotos(ctx, user1ID, hiddenAlbumID, "", 10); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("hidden album error=%v", err)
	}
}
