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

func TestContactReadsUseScopedSummaryAndBoundedFacets(t *testing.T) {
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
	suffix := fmt.Sprintf("contact-read-%d", time.Now().UnixNano())
	firstNSID, secondNSID := suffix+"-a@N00", suffix+"-b@N00"
	groupNSID, albumID, galleryID := suffix+"-group", suffix+"-album", suffix+"-gallery"
	photoBase := time.Now().UnixNano() % 1_000_000_000_000_000
	photoIDs := make([]int64, 5)
	for i := range photoIDs {
		photoIDs[i] = photoBase + int64(i) + 1
	}
	privateCoverID := photoBase + 100
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM person_groups WHERE group_nsid=$1`, groupNSID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM groups WHERE nsid=$1`, groupNSID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=$1`, albumID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM galleries WHERE id=$1`, galleryID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid=$1`, firstNSID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{firstNSID, secondNSID})
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	var keyID, accountID int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'contact integration',$2,'{}',1000,10,20) RETURNING id`, userID, suffix+"-key").Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,'contact owner','{}','{}') RETURNING id`, userID, keyID, suffix+"-account@N00").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := repository.UpsertPeople(ctx, []domain.Person{
		{NSID: firstNSID, Username: "contact-a", Realname: "Contact A", PhotosCount: 12},
		{NSID: secondNSID, Username: "contact-b", Realname: "Contact B", PhotosCount: 6},
	}); err != nil {
		t.Fatal(err)
	}
	for i := range photoIDs {
		updated := checkedAt.Add(time.Duration(len(photoIDs)-i) * time.Minute)
		if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,title,last_update,visibility,sizes,raw,content_hash) VALUES($1,$2,$3,$4,'public',$5,'{}',decode(repeat('00',32),'hex'))`, photoIDs[i], firstNSID, fmt.Sprintf("Contact photo %d", i), updated, []byte(fmt.Sprintf(`[{"url":"https://images.example.invalid/%d.jpg"}]`, photoIDs[i]))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,title,last_update,visibility,sizes,raw,content_hash) VALUES($1,$2,'Private cover',$3,'private',$4,'{}',decode(repeat('11',32),'hex'))`, privateCoverID, firstNSID, checkedAt.Add(-time.Hour), []byte(`[{"url":"https://private.example.invalid/cover.jpg"}]`)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) SELECT $1,$2,'photo',photo_id::text FROM unnest($3::bigint[]) AS photo_id`, userID, accountID, photoIDs); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,starred,paused_at,photos_crawled,favorites_count,sizes_completed,tombstones_count,last_checked_at,last_fave_at)
		VALUES($1,$2,1,'integration',true,NULL,4,3,2,1,$3,$3),($1,$4,2,'integration',false,$3,1,0,0,0,$3,NULL)`, userID, firstNSID, checkedAt, secondNSID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO groups(nsid,name,members_count) VALUES($1,'Contact group',8)`, groupNSID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO person_groups(person_nsid,group_nsid) VALUES($1,$2)`, firstNSID, groupNSID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO albums(id,owner_nsid,title,primary_photo_id,photos_count) VALUES($1,$2,'Contact album',$3,5)`, albumID, firstNSID, privateCoverID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO galleries(id,owner_nsid,title,photos_count) VALUES($1,$2,'Contact gallery',7)`, galleryID, firstNSID); err != nil {
		t.Fatal(err)
	}

	page, err := repository.ListContactInterests(ctx, userID, "all", "", "name", "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].NSID != firstNSID || page.Items[0].PhotosCrawled != 4 || page.Items[0].FavoritesCount != 3 || page.Items[0].SizesPercent != 50 || page.Counts.All != 2 || page.Counts.Starred != 1 || page.NextCursor == nil {
		t.Fatalf("contact interests = %+v, error = %v", page, err)
	}
	starred, err := repository.ListContactInterests(ctx, userID, "starred", "", "name", "", 10)
	if err != nil || len(starred.Items) != 1 || starred.Items[0].NSID != firstNSID {
		t.Fatalf("starred contact interests = %+v, error = %v", starred, err)
	}
	next, err := repository.ListContactInterests(ctx, userID, "all", "", "name", *page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || next.Items[0].NSID != secondNSID {
		t.Fatalf("next contact page = %+v, error = %v", next, err)
	}
	detail, found, err := repository.GetContact(ctx, userID, firstNSID)
	if err != nil || !found || detail.PhotoRecordsCrawled != 4 || detail.SizesCompleted != 50 || detail.Counts.Photos != 12 || len(detail.Facets) != 5 {
		t.Fatalf("contact detail = %+v, found = %t, error = %v", detail, found, err)
	}
	var listedPhotoIDs []string
	var photoCursor string
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		photos, pageErr := repository.ListContactPhotos(ctx, userID, firstNSID, photoCursor, 2)
		if pageErr != nil {
			t.Fatalf("contact photo page %d: %v", pageNumber+1, pageErr)
		}
		for _, photo := range photos.Items {
			listedPhotoIDs = append(listedPhotoIDs, photo.ID)
		}
		if photos.NextCursor == nil {
			break
		}
		photoCursor = *photos.NextCursor
	}
	wantPhotoIDs := make([]string, 0, len(photoIDs))
	for _, photoID := range photoIDs {
		wantPhotoIDs = append(wantPhotoIDs, fmt.Sprint(photoID))
	}
	if fmt.Sprint(listedPhotoIDs) != fmt.Sprint(wantPhotoIDs) {
		t.Fatalf("contact photos across pages = %v, want %v", listedPhotoIDs, wantPhotoIDs)
	}
	for i, photoID := range photoIDs {
		if _, err := pool.Exec(ctx, `INSERT INTO favorites(person_nsid,photo_id,fave_date) VALUES($1,$2,$3)`, firstNSID, photoID, checkedAt.Add(time.Duration(len(photoIDs)-i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var listedFavoriteIDs []string
	var favoriteCursor string
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		favorites, pageErr := repository.ListContactFavorites(ctx, userID, firstNSID, favoriteCursor, 2)
		if pageErr != nil {
			t.Fatalf("contact favorite page %d: %v", pageNumber+1, pageErr)
		}
		for _, favorite := range favorites.Items {
			listedFavoriteIDs = append(listedFavoriteIDs, favorite.ID)
		}
		if favorites.NextCursor == nil {
			break
		}
		favoriteCursor = *favorites.NextCursor
	}
	if fmt.Sprint(listedFavoriteIDs) != fmt.Sprint(wantPhotoIDs) {
		t.Fatalf("contact favorites across pages = %v, want %v", listedFavoriteIDs, wantPhotoIDs)
	}
	albums, err := repository.ListContactAlbums(ctx, userID, firstNSID)
	if err != nil || albums.Total != 1 || albums.Items[0].ID != albumID {
		t.Fatalf("contact albums = %+v, error = %v", albums, err)
	}
	if len(albums.Items[0].CoverPhotoURLs) != 0 {
		t.Fatalf("private album cover was exposed: %v", albums.Items[0].CoverPhotoURLs)
	}
	galleries, err := repository.ListContactGalleries(ctx, userID, firstNSID)
	if err != nil || galleries.Total != 1 || galleries.Items[0].ID != galleryID {
		t.Fatalf("contact galleries = %+v, error = %v", galleries, err)
	}
	groups, err := repository.ListContactGroups(ctx, userID, firstNSID)
	if err != nil || groups.Total != 1 || groups.Items[0].NSID != groupNSID || groups.Items[0].PoolCrawlEnabled {
		t.Fatalf("contact groups = %+v, error = %v", groups, err)
	}
	preference, found, err := repository.SetContactGroupPoolCrawl(ctx, userID, firstNSID, groupNSID, true)
	if err != nil || !found || !preference.Enabled {
		t.Fatalf("group crawl preference = %+v, found = %t, error = %v", preference, found, err)
	}
	var workKind, workSubject string
	var workCursor []byte
	if err := pool.QueryRow(ctx, `SELECT kind,subject_key,cursor FROM work_items WHERE user_id=$1 AND kind='group_pool' AND subject_key=$2`, userID, groupNSID).Scan(&workKind, &workSubject, &workCursor); err != nil {
		t.Fatalf("enabled group preference did not enqueue scheduler work: %v", err)
	}
	var cursor struct {
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal(workCursor, &cursor); err != nil || workKind != "group_pool" || workSubject != groupNSID || cursor.Method != "flickr.groups.pools.getPhotos" || cursor.Params["group_id"] != groupNSID {
		t.Fatalf("group pool work item = kind %q subject %q cursor %+v err=%v", workKind, workSubject, cursor, err)
	}
	groups, err = repository.ListContactGroups(ctx, userID, firstNSID)
	if err != nil || len(groups.Items) != 1 || !groups.Items[0].PoolCrawlEnabled {
		t.Fatalf("updated contact groups = %+v, error = %v", groups, err)
	}
	priorityBase := photoBase + 1000
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,title,last_update,visibility,raw,content_hash)
		SELECT photo_id,$3,'Priority photo',now(),'public','{}',decode(repeat('22',32),'hex')
		FROM generate_series($1::bigint,$2::bigint) AS photo_id`, priorityBase, priorityBase+500, firstNSID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor,direct_action)
		SELECT 'photo_sizes',photo_id::text,$3,'ready',1,'{"page":1,"method":"flickr.photos.getSizes"}'::jsonb,FALSE
		FROM generate_series($1::bigint,$2::bigint) AS photo_id`, priorityBase, priorityBase+500, userID); err != nil {
		t.Fatal(err)
	}
	prioritised, err := repository.PrioritiseContactSizes(ctx, userID, firstNSID)
	if err != nil || prioritised != 501 {
		t.Fatalf("prioritised size work=%d err=%v, want 501", prioritised, err)
	}
	var prioritisedRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM work_items wi JOIN photos ph ON ph.id::text=wi.subject_key
		WHERE wi.user_id=$1 AND wi.kind='photo_sizes' AND ph.id BETWEEN $2 AND $3 AND wi.direct_action`, userID, priorityBase, priorityBase+500).Scan(&prioritisedRows); err != nil || prioritisedRows != 501 {
		t.Fatalf("direct size work=%d err=%v, want all 501 rows", prioritisedRows, err)
	}
	about, found, err := repository.GetContactAbout(ctx, userID, firstNSID)
	if err != nil || !found || about.TombstoneCount != 1 || len(about.Facets) != 5 {
		t.Fatalf("contact about = %+v, found = %t, error = %v", about, found, err)
	}
	if _, found, err := repository.GetContact(ctx, userID+1, firstNSID); err != nil || found {
		t.Fatalf("other user's contact found = %t, error = %v", found, err)
	}
}
