package postgres_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	cataloggen "github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

type contactsQueryCapture struct {
	cataloggen.DBTX
	query string
	args  []any
}

func (db *contactsQueryCapture) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	db.query, db.args = query, append([]any(nil), args...)
	return db.DBTX.Query(ctx, query, args...)
}

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
	user1NSID, user1SecondNSID, user1ThirdNSID := suffix+"-u1", suffix+"-u1-second", suffix+"-u1-third"
	user2NSID, hiddenNSID := suffix+"-u2", suffix+"-hidden"
	personIDs := []string{user1NSID, user1SecondNSID, user1ThirdNSID, user2NSID, hiddenNSID}
	photoIDs := []int64{
		time.Now().UnixNano(), time.Now().UnixNano() + 1, time.Now().UnixNano() + 2,
		time.Now().UnixNano() + 3, time.Now().UnixNano() + 4,
	}
	favoriteTiePhotoID := time.Now().UnixNano() + 100
	cleanupPhotoIDs := append(append([]int64{}, photoIDs...), favoriteTiePhotoID)
	extraPhotoIDs := make([]int64, 7)
	for i := range extraPhotoIDs {
		extraPhotoIDs[i] = time.Now().UnixNano() + int64(i) + 10
	}
	albumID, hiddenAlbumID := suffix+"-album", suffix+"-hidden-album"
	contactAlbumIDs := []string{
		suffix + "-contact-album-1",
		suffix + "-contact-album-2",
		suffix + "-contact-album-3",
		suffix + "-contact-album-null-1",
		suffix + "-contact-album-null-2",
	}
	var user1ID, user2ID, account1ID, account1SecondID, account1ThirdID, account2ID int64
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=ANY($1) OR contact_nsid=ANY($1)`, personIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=ANY($1)`, append([]string{albumID, hiddenAlbumID}, contactAlbumIDs...))
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1)`, append(cleanupPhotoIDs, extraPhotoIDs...))
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
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'user-one-second','{}','{}','ok') RETURNING id`, user1ID, key1ID, user1SecondNSID).Scan(&account1SecondID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'user-one-third','{}','{}','ok') RETURNING id`, user1ID, key1ID, user1ThirdNSID).Scan(&account1ThirdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'user-two','{}','{}','ok') RETURNING id`, user2ID, key2ID, user2NSID).Scan(&account2ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, user2ID); err != nil {
		t.Fatal(err)
	}
	people := []domain.Person{
		{NSID: user1NSID, Username: "one"},
		{NSID: user1SecondNSID, Username: "one-second"},
		{NSID: user1ThirdNSID, Username: "one-third"},
		{NSID: user2NSID, Username: "two"},
		{NSID: hiddenNSID, Username: "hidden"},
	}
	if err := repository.UpsertPeople(ctx, people); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	photos := []domain.Photo{
		{ID: photoIDs[0], OwnerNSID: user1NSID, Title: "private to one", LastUpdate: now, Visibility: domain.VisibilityPrivate, Raw: []byte(`{"id":1}`)},
		{ID: photoIDs[1], OwnerNSID: user2NSID, Title: "shared public", LastUpdate: now.Add(-time.Second), Visibility: domain.VisibilityPublic, Tags: []string{"shared"}, Sizes: []byte(`[{"label":"Large","width":1200,"height":800}]`), SizesStatus: domain.SizesConfirmed, Raw: []byte(`{"id":2}`)},
		{ID: photoIDs[2], OwnerNSID: hiddenNSID, Title: "private to two", LastUpdate: now.Add(-2 * time.Second), Visibility: domain.VisibilityPrivate, Raw: []byte(`{"id":3}`)},
		{ID: photoIDs[3], OwnerNSID: user2NSID, Title: "shared public second", LastUpdate: now.Add(-3 * time.Second), Visibility: domain.VisibilityPublic, Raw: []byte(`{"id":4}`)},
		{ID: photoIDs[4], OwnerNSID: user2NSID, Title: "shared public third", LastUpdate: now.Add(-4 * time.Second), Visibility: domain.VisibilityPublic, Raw: []byte(`{"id":5}`)},
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
		{user2ID, account2ID, "photo", fmt.Sprint(photoIDs[3])},
		{user2ID, account2ID, "photo", fmt.Sprint(photoIDs[4])},
	}
	for _, observation := range observations {
		if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,$3,$4)`, observation.userID, observation.accountID, observation.kind, observation.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.UpsertFavoritesBatch(ctx, []postgres.Favorite{
		{PersonNSID: user1NSID, PhotoID: photoIDs[1], FaveDate: now},
		{PersonNSID: user1NSID, PhotoID: photoIDs[2], FaveDate: now.Add(-time.Second)},
		{PersonNSID: user1NSID, PhotoID: photoIDs[4], FaveDate: now.Add(-3 * time.Second)},
		{PersonNSID: user1SecondNSID, PhotoID: photoIDs[3], FaveDate: now.Add(-2 * time.Second)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertAlbumsBatch(ctx, []postgres.Album{
		{ID: albumID, OwnerNSID: user1NSID, Title: "Mixed", PrimaryPhotoID: &photoIDs[2], PhotosCount: 2, UpdatedAt: now},
		{ID: hiddenAlbumID, OwnerNSID: user1NSID, Title: "Hidden", PrimaryPhotoID: &photoIDs[2], PhotosCount: 1, UpdatedAt: now.Add(-time.Second)},
		{ID: contactAlbumIDs[0], OwnerNSID: user2NSID, Title: "Newest contact album", PhotosCount: 12, UpdatedAt: now.Add(2 * time.Second)},
		{ID: contactAlbumIDs[1], OwnerNSID: user2NSID, Title: "Second contact album", PhotosCount: 0, UpdatedAt: now.Add(time.Second)},
		{ID: contactAlbumIDs[2], OwnerNSID: user2NSID, Title: "Third contact album", PhotosCount: 0, UpdatedAt: now},
		{ID: contactAlbumIDs[3], OwnerNSID: user2NSID, Title: "Null timestamp one", PhotosCount: 0, UpdatedAt: now},
		{ID: contactAlbumIDs[4], OwnerNSID: user2NSID, Title: "Null timestamp two", PhotosCount: 0, UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE albums SET updated_at=NULL WHERE id=ANY($1)`, contactAlbumIDs[3:]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(person_nsid,contact_nsid) VALUES($1,$2)`, user1NSID, user2NSID); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertAlbumPhotosBatch(ctx, []postgres.AlbumPhoto{
		{AlbumID: albumID, PhotoID: photoIDs[1], Position: 0},
		{AlbumID: albumID, PhotoID: photoIDs[2], Position: 1},
		{AlbumID: hiddenAlbumID, PhotoID: photoIDs[2], Position: 0},
		{AlbumID: contactAlbumIDs[0], PhotoID: photoIDs[1], Position: 0},
		{AlbumID: contactAlbumIDs[1], PhotoID: photoIDs[1], Position: 0},
		{AlbumID: contactAlbumIDs[2], PhotoID: photoIDs[1], Position: 0},
		{AlbumID: contactAlbumIDs[3], PhotoID: photoIDs[1], Position: 0},
		{AlbumID: contactAlbumIDs[4], PhotoID: photoIDs[1], Position: 0},
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
	if len(visiblePeople) != 2 || visiblePeople[user1NSID] != 1 || visiblePeople[user2NSID] != 3 {
		t.Fatalf("user one visible people/counts=%v", visiblePeople)
	}
	if _, err := reads.GetPerson(ctx, user1ID, hiddenNSID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("hidden person error=%v", err)
	}
	personPhotos, _, err := reads.ListPersonPhotos(ctx, user1ID, user2NSID, "", 10)
	if err != nil || len(personPhotos.Items) != 3 || personPhotos.Items[0].ID != photoIDs[1] || personPhotos.Items[0].SizesStatus != "confirmed" || personPhotos.Items[0].Sizes[0].Label != "Large" {
		t.Fatalf("visible person photos=%+v err=%v", personPhotos, err)
	}
	if _, err := reads.GetPhoto(ctx, user2ID, photoIDs[0]); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("photo hidden from user two error=%v", err)
	}
	if photo, err := reads.GetPhoto(ctx, user1ID, photoIDs[1]); err != nil || photo.ID != photoIDs[1] {
		t.Fatalf("shared photo=%+v err=%v", photo, err)
	}
	favorites, favoritesCursor, err := reads.ListFavorites(ctx, user1ID, "", 2)
	if err != nil || len(favorites.Items) != 2 || favorites.Items[0].Photo.ID != photoIDs[1] || favorites.Items[1].Photo.ID != photoIDs[3] || favoritesCursor == "" {
		t.Fatalf("visible favorites=%+v err=%v", favorites, err)
	}
	favorites, _, err = reads.ListFavorites(ctx, user1ID, favoritesCursor, 2)
	if err != nil || len(favorites.Items) != 1 || favorites.Items[0].Photo.ID != photoIDs[4] {
		t.Fatalf("second visible favorites page=%+v err=%v", favorites, err)
	}

	filteredPhotos := make([]domain.Photo, len(extraPhotoIDs))
	for i, id := range extraPhotoIDs {
		owner := hiddenNSID
		visibility := domain.VisibilityPrivate
		if i >= 5 {
			owner = user1NSID
		}
		filteredPhotos[i] = domain.Photo{
			ID: id, OwnerNSID: owner, Title: fmt.Sprintf("filtered favorite %d", i),
			LastUpdate: now.Add(-time.Duration(i+10) * time.Second), Visibility: visibility, Raw: []byte(`{"id":1}`),
		}
	}
	if err := repository.UpsertPhotos(ctx, filteredPhotos); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertFavoritesBatch(ctx, []postgres.Favorite{
		{PersonNSID: user1NSID, PhotoID: extraPhotoIDs[0], FaveDate: now.Add(-4 * time.Second)},
		{PersonNSID: user1NSID, PhotoID: extraPhotoIDs[1], FaveDate: now.Add(-5 * time.Second)},
		{PersonNSID: user1NSID, PhotoID: extraPhotoIDs[2], FaveDate: now.Add(-6 * time.Second)},
		{PersonNSID: user1NSID, PhotoID: extraPhotoIDs[3], FaveDate: now.Add(-7 * time.Second)},
		{PersonNSID: user1NSID, PhotoID: extraPhotoIDs[4], FaveDate: now.Add(-8 * time.Second)},
		{PersonNSID: user1NSID, PhotoID: extraPhotoIDs[5], FaveDate: now.Add(-9 * time.Second)},
		{PersonNSID: user1NSID, PhotoID: extraPhotoIDs[6], FaveDate: now.Add(-10 * time.Second)},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE favorites SET removed_at=now() WHERE person_nsid=$1 AND photo_id=ANY($2)`, user1NSID, extraPhotoIDs[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE photos SET tombstoned_at=now() WHERE id=ANY($1)`, extraPhotoIDs[2:4]); err != nil {
		t.Fatal(err)
	}
	for _, id := range extraPhotoIDs[5:] {
		if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, user1ID, account1ID, fmt.Sprint(id)); err != nil {
			t.Fatal(err)
		}
	}
	visibility := sharingapp.Filter(sharingdomain.User{ID: user1ID}).SQL
	filteredCursor := &app.FavoriteCursor{FaveDate: now.Add(-3 * time.Second), PhotoID: photoIDs[4], FlickrAccountID: account1ID}
	filteredPage, err := repository.ListVisibleFavorites(ctx, user1ID, visibility, filteredCursor, 2)
	if err != nil || len(filteredPage.Items) != 0 || filteredPage.NextCursor == nil ||
		!filteredPage.NextCursor.FaveDate.Equal(now.Add(-6*time.Second)) || filteredPage.NextCursor.PhotoID != extraPhotoIDs[2] {
		t.Fatalf("mostly filtered first batch=%+v err=%v", filteredPage, err)
	}
	filteredPage, err = repository.ListVisibleFavorites(ctx, user1ID, visibility, filteredPage.NextCursor, 2)
	if err != nil || len(filteredPage.Items) != 1 || filteredPage.Items[0].Photo.ID != extraPhotoIDs[5] || filteredPage.NextCursor == nil {
		t.Fatalf("mostly filtered second batch=%+v err=%v", filteredPage, err)
	}
	filteredPage, err = repository.ListVisibleFavorites(ctx, user1ID, visibility, filteredPage.NextCursor, 2)
	if err != nil || len(filteredPage.Items) != 1 || filteredPage.Items[0].Photo.ID != extraPhotoIDs[6] || filteredPage.NextCursor != nil {
		t.Fatalf("mostly filtered final batch=%+v err=%v", filteredPage, err)
	}
	if err := repository.UpsertPhotos(ctx, []domain.Photo{{
		ID: favoriteTiePhotoID, OwnerNSID: user1NSID, Title: "same-time favorite",
		LastUpdate: now, Visibility: domain.VisibilityPrivate, Raw: []byte(`{"id":99}`),
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, user1ID, account1ID, fmt.Sprint(favoriteTiePhotoID)); err != nil {
		t.Fatal(err)
	}
	tieDate := now.Add(time.Second)
	if err := repository.UpsertFavoritesBatch(ctx, []postgres.Favorite{
		{PersonNSID: user1NSID, PhotoID: favoriteTiePhotoID, FaveDate: tieDate},
		{PersonNSID: user1SecondNSID, PhotoID: favoriteTiePhotoID, FaveDate: tieDate},
		{PersonNSID: user1ThirdNSID, PhotoID: favoriteTiePhotoID, FaveDate: tieDate},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE favorites SET removed_at=now() WHERE person_nsid=$1 AND photo_id=$2`, user1ThirdNSID, favoriteTiePhotoID); err != nil {
		t.Fatal(err)
	}
	tiedFavorites, tiedCursor, err := reads.ListFavorites(ctx, user1ID, "", 1)
	if err != nil || len(tiedFavorites.Items) != 1 || tiedFavorites.Items[0].Photo.ID != favoriteTiePhotoID || tiedCursor == "" {
		t.Fatalf("first equal-key favorites page=%+v cursor=%q err=%v", tiedFavorites, tiedCursor, err)
	}
	encodedCursor, err := base64.RawURLEncoding.DecodeString(tiedCursor)
	if err != nil {
		t.Fatalf("decode equal-key favorites cursor: %v", err)
	}
	var cursorEnvelope struct {
		Value struct {
			FlickrAccountID int64 `json:"flickr_account_id"`
		} `json:"c"`
	}
	if err := json.Unmarshal(encodedCursor, &cursorEnvelope); err != nil {
		t.Fatalf("unmarshal equal-key favorites cursor: %v", err)
	}
	if cursorEnvelope.Value.FlickrAccountID != account1SecondID {
		t.Fatalf("cursor account id=%d, want boundary account %d", cursorEnvelope.Value.FlickrAccountID, account1SecondID)
	}
	tiedFavorites, _, err = reads.ListFavorites(ctx, user1ID, tiedCursor, 1)
	if err != nil || len(tiedFavorites.Items) != 1 || tiedFavorites.Items[0].Photo.ID != favoriteTiePhotoID {
		t.Fatalf("second equal-key favorites page=%+v err=%v", tiedFavorites, err)
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
	contactPage, contactCursor, err := reads.ListContactAlbums(ctx, user1ID, "", "", 2)
	if err != nil || len(contactPage.Items) != 2 || contactPage.Items[0].ID != contactAlbumIDs[0] || contactPage.Items[1].ID != contactAlbumIDs[1] || contactCursor == "" {
		t.Fatalf("first contact album page=%+v cursor=%q err=%v", contactPage, contactCursor, err)
	}
	contactPage, contactCursor, err = reads.ListContactAlbums(ctx, user1ID, "", contactCursor, 2)
	if err != nil || len(contactPage.Items) != 2 || contactPage.Items[0].ID != contactAlbumIDs[2] || contactPage.Items[1].ID != contactAlbumIDs[4] || contactPage.Items[1].UpdatedAt != nil || contactCursor == "" {
		t.Fatalf("second contact album page=%+v err=%v", contactPage, err)
	}
	contactPage, contactCursor, err = reads.ListContactAlbums(ctx, user1ID, "", contactCursor, 2)
	if err != nil || len(contactPage.Items) != 1 || contactPage.Items[0].ID != contactAlbumIDs[3] || contactPage.Items[0].UpdatedAt != nil || contactCursor != "" {
		t.Fatalf("third contact album page=%+v cursor=%q err=%v", contactPage, contactCursor, err)
	}
}

func TestContactAlbumListRequiresVisiblePhotoAndIsolatesUsers(t *testing.T) {
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
	suffix := fmt.Sprintf("contact-album-visibility-%d", time.Now().UnixNano())
	userNSIDs := []string{suffix + "-user", suffix + "-contact"}
	photoStart := time.Now().UnixNano()
	photoIDs := []int64{photoStart, photoStart + 1, photoStart + 2}
	albumIDs := []string{suffix + "-visible", suffix + "-hidden", suffix + "-not-contact"}
	var userIDs, accountIDs [2]int64
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=ANY($1) OR contact_nsid=ANY($1)`, userNSIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=ANY($1)`, albumIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1)`, photoIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, userNSIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=ANY($1)`, userIDs[:])
	}()
	for i := range userIDs {
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, fmt.Sprintf("%s-%d@example.invalid", suffix, i)).Scan(&userIDs[i]); err != nil {
			t.Fatal(err)
		}
		var keyID int64
		if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'test',$2,'{}',1000,10,20) RETURNING id`, userIDs[i], fmt.Sprintf("%s-key-%d", suffix, i)).Scan(&keyID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,$4,'{}','{}','ok') RETURNING id`, userIDs[i], keyID, userNSIDs[i], fmt.Sprintf("user-%d", i)).Scan(&accountIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	var sharingEnabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM sharing_settings WHERE user_id=$1`, userIDs[1]).Scan(&sharingEnabled); err != nil {
		t.Fatal(err)
	}
	if sharingEnabled {
		t.Fatal("contact sharing is enabled; expected the default disabled policy")
	}
	people := []domain.Person{{NSID: userNSIDs[0], Username: "user"}, {NSID: userNSIDs[1], Username: "contact"}}
	if err := repository.UpsertPeople(ctx, people); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	photos := []domain.Photo{
		{ID: photoIDs[0], OwnerNSID: userNSIDs[1], Title: "visible", LastUpdate: now, Visibility: domain.VisibilityPublic, Raw: []byte(`{"id":1}`)},
		{ID: photoIDs[1], OwnerNSID: userNSIDs[1], Title: "unshared public", LastUpdate: now.Add(-time.Second), Visibility: domain.VisibilityPublic, Raw: []byte(`{"id":2}`)},
		{ID: photoIDs[2], OwnerNSID: userNSIDs[1], Title: "private", LastUpdate: now.Add(-2 * time.Second), Visibility: domain.VisibilityPrivate, Raw: []byte(`{"id":3}`)},
	}
	if err := repository.UpsertPhotos(ctx, photos); err != nil {
		t.Fatal(err)
	}
	observations := []struct {
		userID, accountID int64
		photoID           int64
	}{
		{userIDs[0], accountIDs[0], photoIDs[0]},
		{userIDs[1], accountIDs[1], photoIDs[0]},
		{userIDs[1], accountIDs[1], photoIDs[1]},
		{userIDs[1], accountIDs[1], photoIDs[2]},
	}
	for _, observation := range observations {
		if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, observation.userID, observation.accountID, fmt.Sprint(observation.photoID)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(person_nsid,contact_nsid) VALUES($1,$2)`, userNSIDs[0], userNSIDs[1]); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertAlbumsBatch(ctx, []postgres.Album{
		{ID: albumIDs[0], OwnerNSID: userNSIDs[1], Title: "Visible album", PhotosCount: 99, UpdatedAt: now},
		{ID: albumIDs[1], OwnerNSID: userNSIDs[1], Title: "Hidden album", PhotosCount: 77, UpdatedAt: now.Add(-time.Second)},
		{ID: albumIDs[2], OwnerNSID: userNSIDs[0], Title: "Not a contact album", PhotosCount: 0, UpdatedAt: now.Add(-2 * time.Second)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertAlbumPhotosBatch(ctx, []postgres.AlbumPhoto{
		{AlbumID: albumIDs[0], PhotoID: photoIDs[0], Position: 0},
		{AlbumID: albumIDs[0], PhotoID: photoIDs[1], Position: 1},
		{AlbumID: albumIDs[0], PhotoID: photoIDs[2], Position: 2},
		{AlbumID: albumIDs[1], PhotoID: photoIDs[1], Position: 0},
	}); err != nil {
		t.Fatal(err)
	}

	page, _, err := reads.ListContactAlbums(ctx, userIDs[0], "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != albumIDs[0] {
		t.Fatalf("contact album list=%+v, want only the album containing a photo visible to the user", page.Items)
	}

	otherUserPage, _, err := reads.ListContactAlbums(ctx, userIDs[1], "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherUserPage.Items) != 0 {
		t.Fatalf("other user's contact albums=%+v, want no albums without a contact relationship", otherUserPage.Items)
	}
}

func TestCatalogReadVisibilityPlanAvoidsSequentialScans(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	suffix := fmt.Sprintf("catalog-plan-%d", time.Now().UnixNano())
	var userID, keyID, accountID int64
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'plan',$2,'{}',1000,10,20) RETURNING id`, userID, suffix+"-key").Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'plan','{}','{}','ok') RETURNING id`, userID, keyID, suffix+"-owner").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	ownerNSID := suffix + "-owner"
	if _, err := tx.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'plan')`, ownerNSID); err != nil {
		t.Fatal(err)
	}
	const fixtureRows = 20000
	basePhotoID := time.Now().UnixNano()
	if _, err := tx.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,raw,content_hash)
SELECT $1::bigint + item, $2, now(), 'private', '{}'::jsonb, decode(repeat('00',32),'hex')
FROM generate_series(1,$3) AS item`, basePhotoID, ownerNSID, fixtureRows); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id)
SELECT $1,$2,'photo',($3::bigint + item)::text FROM generate_series(1,$4) AS item`, userID, accountID, basePhotoID, fixtureRows); err != nil {
		t.Fatal(err)
	}
	var targetPhotoID int64
	if err := tx.QueryRow(ctx, `SELECT record_id::bigint FROM observations
WHERE user_id=$1 AND flickr_account_id=$2 AND record_kind='photo'
AND tableoid='public.observations_p00'::regclass LIMIT 1`, userID, accountID).Scan(&targetPhotoID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE photos`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE observations_p00`); err != nil {
		t.Fatal(err)
	}

	visibility := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	query := `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT photos.id FROM photos
WHERE photos.id=ANY($2) AND photos.tombstoned_at IS NULL AND ` + visibility.Expression
	args := append(append([]any{}, visibility.Args...), []int64{targetPhotoID})
	var rawPlan []byte
	if err := tx.QueryRow(ctx, query, args...).Scan(&rawPlan); err != nil {
		t.Fatalf("explain catalog photo visibility lookup: %v", err)
	}
	var explained []struct {
		Plan catalogReadPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal(rawPlan, &explained); err != nil {
		t.Fatalf("decode catalog photo visibility plan: %v", err)
	}
	if len(explained) == 0 {
		t.Fatalf("empty catalog photo visibility plan: %s", rawPlan)
	}
	seqScan, observationIndex := inspectCatalogReadPlan(explained[0].Plan)
	if seqScan || !observationIndex {
		t.Fatalf("catalog photo visibility plan must avoid Seq Scans on observations/photos and use the observations lookup index: %s", rawPlan)
	}
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) catalog photo visibility on %d fixture rows: %s", fixtureRows, rawPlan)
}

type catalogReadPlanNode struct {
	NodeType  string                `json:"Node Type"`
	Relation  string                `json:"Relation Name"`
	IndexName string                `json:"Index Name"`
	Plans     []catalogReadPlanNode `json:"Plans"`
}

func inspectCatalogReadPlan(node catalogReadPlanNode) (seqScan, observationIndex bool) {
	seqScan = (node.NodeType == "Seq Scan" || node.NodeType == "Parallel Seq Scan") &&
		(strings.HasPrefix(node.Relation, "observations") || strings.HasPrefix(node.Relation, "photos"))
	observationIndex = node.IndexName == "observations_p00_kind_record_idx"
	for _, child := range node.Plans {
		childSeqScan, childObservationIndex := inspectCatalogReadPlan(child)
		seqScan = seqScan || childSeqScan
		observationIndex = observationIndex || childObservationIndex
	}
	return seqScan, observationIndex
}

func TestContactsKeysetPageUsesReverseIndexAndBoundedPlan(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	suffix := fmt.Sprintf("contacts-plan-%d", time.Now().UnixNano())
	account := suffix + "-account"
	contacts := make([]string, 5000)
	for i := range contacts {
		contacts[i] = fmt.Sprintf("%s-%05d", suffix, i)
	}
	people := append([]string{account}, contacts...)
	var userID, keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=ANY($2)`, account, contacts)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, people)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	}()
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'test',$2,'{}',1000,10,20) RETURNING id`, userID, suffix+"-key").Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'account','{}','{}','ok')`, userID, keyID, account); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) SELECT nsid,nsid FROM unnest($1::text[]) AS people(nsid)`, people); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(person_nsid,contact_nsid) SELECT $1, contact_nsid FROM unnest($2::text[]) AS contacts(contact_nsid)`, account, contacts); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE contacts`); err != nil {
		t.Fatal(err)
	}
	captured := &contactsQueryCapture{DBTX: pool}
	repository, err := postgres.New(captured)
	if err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListContacts(ctx, userID, nil, 25)
	if err != nil || len(page.Items) != 25 || page.NextCursor == nil {
		t.Fatalf("contacts page items=%d next=%v err=%v", len(page.Items), page.NextCursor, err)
	}
	var plan []any
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+captured.query, captured.args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode explain plan: %v", err)
	}
	if !contactsPagePlanBounded(plan) {
		t.Fatal("contacts page plan did not stop after 26 indexed, distinct contact rows")
	}
}

func contactsPagePlanBounded(value any) bool {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if contactsPagePlanBounded(child) {
				return true
			}
		}
	case map[string]any:
		if node["Node Type"] == "Limit" && node["Parent Relationship"] == "InitPlan" {
			rows, rowsOK := node["Actual Rows"].(float64)
			children, childrenOK := node["Plans"].([]any)
			if rowsOK && rows == 26 && childrenOK && len(children) == 1 {
				unique, uniqueOK := children[0].(map[string]any)
				if uniqueOK && unique["Node Type"] == "Unique" {
					uniqueRows, uniqueRowsOK := unique["Actual Rows"].(float64)
					encoded, err := json.Marshal(unique)
					uniquePlan := string(encoded)
					return uniqueRowsOK && uniqueRows == 26 && err == nil &&
						strings.Contains(uniquePlan, "contacts_contact_nsid_person_nsid_idx") &&
						!strings.Contains(uniquePlan, `"Node Type":"Sort"`) &&
						!strings.Contains(uniquePlan, "Aggregate")
				}
			}
		}
		for _, child := range node {
			if contactsPagePlanBounded(child) {
				return true
			}
		}
	}
	return false
}
