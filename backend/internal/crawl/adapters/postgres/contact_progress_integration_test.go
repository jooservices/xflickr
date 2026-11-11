package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
)

func TestContactProgressStaysAccurateAcrossRecrawlsAndReconciliation(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	person := "progress-" + suffix + "@N01"
	base := int64(720000000000000000) + time.Now().UnixNano()%1_000_000*10
	ids := []int64{base + 1, base + 2, base + 3, base + 4, base + 5, base + 6}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1)`, ids)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person)
	})
	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	lastUpdate := checkedAt.Add(-time.Hour)
	var keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'progress integration',$2,'{}',1000,10,20) RETURNING id`, userID, suffix+"-key").Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	var accountID int64
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,'progress owner','{}','{}') RETURNING id`, userID, keyID, person).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'integration')`, userID, person); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'progress owner')`, person); err != nil {
		t.Fatal(err)
	}
	repository, err := catalogpg.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	photos := make([]catalog.Photo, 0, len(ids))
	for _, id := range ids {
		photos = append(photos, catalog.Photo{ID: id, OwnerNSID: person, LastUpdate: lastUpdate, Visibility: catalog.VisibilityPublic, SizesStatus: catalog.SizesConfirmed, SizesCheckedAt: &checkedAt, Sizes: []byte(`[]`), Raw: []byte(`{}`)})
	}
	if err := repository.UpsertPhotos(ctx, photos); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id)
		SELECT $1,$2,'photo',photo_id::text FROM unnest($3::bigint[]) AS photo_id`, userID, accountID, []int64{ids[0], ids[1], ids[2], ids[4]}); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	progress := contactProgressTx{tx: tx}
	txRepository, err := catalogpg.New(tx)
	if err != nil {
		t.Fatal(err)
	}
	photoPage := func(page int, values []int64, incremental bool) {
		t.Helper()
		params := map[string]string{"per_page": "2"}
		if incremental {
			params["min_upload_date"] = "1"
		}
		item := domain.Item{Kind: "photos", UserID: userID, Subject: person, Cursor: domain.Cursor{Page: page, Params: params}}
		result := domain.Result{Photos: make([]catalog.Photo, 0, len(values))}
		for _, id := range values {
			result.Photos = append(result.Photos, catalog.Photo{ID: id})
		}
		if err := progress.BeginPage(ctx, item, result); err != nil {
			t.Fatal(err)
		}
		if err := progress.Record(ctx, item, result); err != nil {
			t.Fatal(err)
		}
		if incremental {
			for _, id := range values {
				if _, err := tx.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3) ON CONFLICT DO NOTHING`, userID, accountID, fmt.Sprint(id)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	photoPage(1, ids[:2], false)
	photoPage(2, ids[2:3], false)
	photoPage(1, ids[:2], false)
	photoPage(2, ids[2:3], false)
	photoPage(1, ids[2:4], true)
	photoPage(1, ids[2:4], true)
	if got := summaryValue(t, ctx, tx, `photos_crawled`, userID, person); got != 4 {
		t.Fatalf("photos_crawled=%d, want stable count 4", got)
	}
	if got := summaryValue(t, ctx, tx, `sizes_completed`, userID, person); got != 4 {
		t.Fatalf("sizes_completed=%d, want stable count 4", got)
	}

	if err := repository.UpsertFavoritesBatch(ctx, []catalogpg.Favorite{{PersonNSID: person, PhotoID: ids[0], FaveDate: checkedAt}, {PersonNSID: person, PhotoID: ids[1], FaveDate: checkedAt.Add(-time.Minute)}, {PersonNSID: person, PhotoID: ids[2], FaveDate: checkedAt.Add(-2 * time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	favoritePage := func(page int, values []int64, incremental bool) {
		t.Helper()
		params := map[string]string{"per_page": "2"}
		if incremental {
			params["min_fave_date"] = "1"
		}
		item := domain.Item{Kind: "favorites", UserID: userID, Subject: person, Cursor: domain.Cursor{Page: page, Params: params}}
		result := domain.Result{Favorites: make([]catalog.Favorite, 0, len(values))}
		faves := make([]catalogpg.Favorite, 0, len(values))
		for _, id := range values {
			result.Favorites = append(result.Favorites, catalog.Favorite{PersonNSID: person, PhotoID: id})
			faves = append(faves, catalogpg.Favorite{PersonNSID: person, PhotoID: id, FaveDate: checkedAt})
		}
		if err := progress.BeginPage(ctx, item, result); err != nil {
			t.Fatal(err)
		}
		if err := txRepository.UpsertFavoritesBatch(ctx, faves); err != nil {
			t.Fatal(err)
		}
	}
	favoritePage(1, ids[:2], false)
	favoritePage(2, ids[2:3], false)
	favoritePage(1, ids[:2], false)
	favoritePage(2, ids[2:3], false)
	favoritePage(1, ids[2:4], true)
	favoritePage(1, ids[2:4], true)
	if got := summaryValue(t, ctx, tx, `favorites_count`, userID, person); got != 4 {
		t.Fatalf("favorites_count=%d, want stable count 4", got)
	}

	for _, id := range ids[4:] {
		if _, err := tx.Exec(ctx, `UPDATE photos SET last_seen_at=$2 WHERE id=$1`, id, checkedAt.Add(-48*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE favorites SET seen_at=$3 WHERE person_nsid=$1 AND photo_id=$2`, person, ids[0], checkedAt.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := (reconcileTx{tx: tx}).Complete(ctx, userID, person, checkedAt.Add(-24*time.Hour), checkedAt); err != nil {
		t.Fatal(err)
	}
	if got := summaryValue(t, ctx, tx, `tombstones_count`, userID, person); got != 2 {
		t.Fatalf("tombstones_count=%d after reconcile, want 2", got)
	}
	if got := summaryValue(t, ctx, tx, `favorites_count`, userID, person); got != 3 {
		t.Fatalf("favorites_count=%d after reconcile, want 3", got)
	}
	photoPage(1, ids[4:5], false)
	if _, err := tx.Exec(ctx, `UPDATE photos SET tombstoned_at=NULL,last_seen_at=$2 WHERE id=$1`, ids[4], checkedAt); err != nil {
		t.Fatal(err)
	}
	photoPage(1, ids[5:], false)
	if _, err := tx.Exec(ctx, `UPDATE photos SET tombstoned_at=NULL,last_seen_at=$2 WHERE id=$1`, ids[5], checkedAt); err != nil {
		t.Fatal(err)
	}
	if got := summaryValue(t, ctx, tx, `tombstones_count`, userID, person); got != 0 {
		t.Fatalf("tombstones_count=%d after photo revival, want 0", got)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func summaryValue(t *testing.T, ctx context.Context, tx Conn, column string, userID int64, person string) int64 {
	t.Helper()
	var value int64
	if err := tx.QueryRow(ctx, `SELECT `+column+` FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
