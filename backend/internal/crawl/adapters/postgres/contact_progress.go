package postgres

import (
	"context"
	"fmt"
	"strconv"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
)

type contactProgressTx struct{ tx Conn }

func (w contactProgressTx) BeginPage(ctx context.Context, item domain.Item, result domain.Result) error {
	if item.UserID <= 0 || item.Subject == "" {
		return nil
	}
	switch item.Kind {
	case "photos":
		ids := photoIDs(result.Photos)
		newCount, revivedCount, err := w.photoPageCounts(ctx, item, ids)
		if err != nil {
			return err
		}
		incremental := item.Cursor.Params["min_upload_date"] != ""
		count := int64(newCount)
		if !incremental {
			count = pageOffset(item.Cursor, len(ids))
		}
		if _, err := w.tx.Exec(ctx, `UPDATE crawl_interests SET
			photos_crawled=CASE WHEN $3 THEN photos_crawled+$4 ELSE $4 END,
			tombstones_count=GREATEST(0,tombstones_count-$5)
			WHERE user_id=$1 AND person_nsid=$2`, item.UserID, item.Subject, incremental, count, revivedCount); err != nil {
			return fmt.Errorf("crawl: update contact photo progress: %w", err)
		}
	case "favorites":
		ids := favoriteIDs(result.Favorites)
		incremental := item.Cursor.Params["min_fave_date"] != ""
		count := int64(len(ids))
		if incremental && len(ids) > 0 {
			if err := w.tx.QueryRow(ctx, `SELECT count(*) FROM unnest($1::bigint[]) AS page(photo_id)
				WHERE NOT EXISTS (SELECT 1 FROM favorites f WHERE f.person_nsid=$2 AND f.photo_id=page.photo_id AND f.removed_at IS NULL)`, ids, item.Subject).Scan(&count); err != nil {
				return fmt.Errorf("crawl: count new contact favorites: %w", err)
			}
		}
		if !incremental {
			count = pageOffset(item.Cursor, len(ids))
		}
		if _, err := w.tx.Exec(ctx, `UPDATE crawl_interests SET
			favorites_count=CASE WHEN $3 THEN favorites_count+$4 ELSE $4 END
			WHERE user_id=$1 AND person_nsid=$2`, item.UserID, item.Subject, incremental, count); err != nil {
			return fmt.Errorf("crawl: update contact favorites progress: %w", err)
		}
	}
	return nil
}

func (w contactProgressTx) photoPageCounts(ctx context.Context, item domain.Item, ids []int64) (int64, int64, error) {
	if len(ids) == 0 {
		return 0, 0, nil
	}
	var newCount, revivedCount int64
	err := w.tx.QueryRow(ctx, `WITH page_ids AS (SELECT unnest($1::bigint[]) AS id)
		SELECT count(*) FILTER (WHERE NOT EXISTS (
			SELECT 1 FROM observations o WHERE o.user_id=$2 AND o.record_kind='photo' AND o.record_id=page_ids.id::text
		))::bigint,
		count(*) FILTER (WHERE ph.tombstoned_at IS NOT NULL)::bigint
		FROM page_ids LEFT JOIN photos ph ON ph.id=page_ids.id`, ids, item.UserID).Scan(&newCount, &revivedCount)
	if err != nil {
		return 0, 0, fmt.Errorf("crawl: count new contact photos: %w", err)
	}
	return newCount, revivedCount, nil
}

func (w contactProgressTx) Record(ctx context.Context, item domain.Item, result domain.Result) error {
	if item.UserID <= 0 || item.Subject == "" || item.Kind != "photos" {
		return nil
	}
	ids := photoIDs(result.Photos)
	var confirmed int64
	if len(ids) > 0 {
		incremental := item.Cursor.Params["min_upload_date"] != ""
		err := w.tx.QueryRow(ctx, `WITH page_ids AS (SELECT unnest($1::bigint[]) AS id)
			SELECT count(*) FROM page_ids JOIN photos ph ON ph.id=page_ids.id
			WHERE ph.sizes_status='confirmed' AND ph.sizes_checked_at>=ph.last_update
			AND (NOT $3 OR NOT EXISTS (
				SELECT 1 FROM observations o WHERE o.user_id=$2 AND o.record_kind='photo' AND o.record_id=page_ids.id::text
			))`, ids, item.UserID, incremental).Scan(&confirmed)
		if err != nil {
			return fmt.Errorf("crawl: count confirmed contact photo sizes: %w", err)
		}
	}
	reset := item.Cursor.Page <= 1 && item.Cursor.Params["min_upload_date"] == ""
	if _, err := w.tx.Exec(ctx, `UPDATE crawl_interests SET
		sizes_completed=CASE WHEN $3 THEN $4 ELSE sizes_completed+$4 END
		WHERE user_id=$1 AND person_nsid=$2`, item.UserID, item.Subject, reset, confirmed); err != nil {
		return fmt.Errorf("crawl: update contact photo sizes: %w", err)
	}
	return nil
}

func pageOffset(cursor domain.Cursor, count int) int64 {
	if cursor.Page <= 1 {
		return int64(count)
	}
	perPage, err := strconv.ParseInt(cursor.Params["per_page"], 10, 64)
	if err != nil || perPage <= 0 {
		perPage = 500
	}
	return int64(cursor.Page-1)*perPage + int64(count)
}

func photoIDs(values []catalog.Photo) []int64 {
	ids := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, photo := range values {
		if photo.ID <= 0 {
			continue
		}
		if _, ok := seen[photo.ID]; ok {
			continue
		}
		seen[photo.ID] = struct{}{}
		ids = append(ids, photo.ID)
	}
	return ids
}

func favoriteIDs(values []catalog.Favorite) []int64 {
	ids := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, favorite := range values {
		if favorite.PhotoID <= 0 {
			continue
		}
		if _, ok := seen[favorite.PhotoID]; ok {
			continue
		}
		seen[favorite.PhotoID] = struct{}{}
		ids = append(ids, favorite.PhotoID)
	}
	return ids
}
