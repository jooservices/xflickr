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
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM groups WHERE nsid=$1`, groupNSID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=$1`, albumID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM galleries WHERE id=$1`, galleryID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{firstNSID, secondNSID})
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	if err := repository.UpsertPeople(ctx, []domain.Person{
		{NSID: firstNSID, Username: "contact-a", Realname: "Contact A", PhotosCount: 12},
		{NSID: secondNSID, Username: "contact-b", Realname: "Contact B", PhotosCount: 6},
	}); err != nil {
		t.Fatal(err)
	}
	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
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
	if _, err := pool.Exec(ctx, `INSERT INTO albums(id,owner_nsid,title,photos_count) VALUES($1,$2,'Contact album',5)`, albumID, firstNSID); err != nil {
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
	photos, err := repository.ListContactPhotos(ctx, userID, firstNSID, "", 10)
	if err != nil || photos.Total != 4 || len(photos.Items) != 0 {
		t.Fatalf("contact photos = %+v, error = %v", photos, err)
	}
	favorites, err := repository.ListContactFavorites(ctx, userID, firstNSID, "", 10)
	if err != nil || favorites.Total != 3 || len(favorites.Items) != 0 {
		t.Fatalf("contact favorites = %+v, error = %v", favorites, err)
	}
	albums, err := repository.ListContactAlbums(ctx, userID, firstNSID)
	if err != nil || albums.Total != 1 || albums.Items[0].ID != albumID {
		t.Fatalf("contact albums = %+v, error = %v", albums, err)
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
	groups, err = repository.ListContactGroups(ctx, userID, firstNSID)
	if err != nil || len(groups.Items) != 1 || !groups.Items[0].PoolCrawlEnabled {
		t.Fatalf("updated contact groups = %+v, error = %v", groups, err)
	}
	about, found, err := repository.GetContactAbout(ctx, userID, firstNSID)
	if err != nil || !found || about.TombstoneCount != 1 || len(about.Facets) != 5 {
		t.Fatalf("contact about = %+v, found = %t, error = %v", about, found, err)
	}
	if _, found, err := repository.GetContact(ctx, userID+1, firstNSID); err != nil || found {
		t.Fatalf("other user's contact found = %t, error = %v", found, err)
	}
}
