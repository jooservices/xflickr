package postgres

import (
	"context"
	"fmt"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

type contactProgressTx struct{ tx Conn }

func (w contactProgressTx) Record(ctx context.Context, item domain.Item, result domain.Result) error {
	if item.UserID <= 0 || item.Subject == "" {
		return nil
	}
	switch item.Kind {
	case "photos":
		ids := make([]int64, 0, len(result.Photos))
		for _, photo := range result.Photos {
			ids = append(ids, photo.ID)
		}
		var confirmed int64
		if len(ids) > 0 {
			if err := w.tx.QueryRow(ctx, `SELECT count(*) FROM photos WHERE id=ANY($1) AND sizes_status='confirmed' AND sizes_checked_at>=last_update`, ids).Scan(&confirmed); err != nil {
				return fmt.Errorf("crawl: count confirmed contact photo sizes: %w", err)
			}
		}
		reset := item.Cursor.Page <= 1 && item.Cursor.Params["min_upload_date"] == ""
		if _, err := w.tx.Exec(ctx, `UPDATE crawl_interests SET
			photos_crawled=CASE WHEN $3 THEN $4 ELSE photos_crawled+$4 END,
			sizes_completed=CASE WHEN $3 THEN $5 ELSE sizes_completed+$5 END
			WHERE user_id=$1 AND person_nsid=$2`, item.UserID, item.Subject, reset, len(result.Photos), confirmed); err != nil {
			return fmt.Errorf("crawl: update contact photo progress: %w", err)
		}
	case "favorites":
		reset := item.Cursor.Page <= 1 && item.Cursor.Params["min_fave_date"] == ""
		if _, err := w.tx.Exec(ctx, `UPDATE crawl_interests SET
			favorites_count=CASE WHEN $3 THEN $4 ELSE favorites_count+$4 END
			WHERE user_id=$1 AND person_nsid=$2`, item.UserID, item.Subject, reset, len(result.Favorites)); err != nil {
			return fmt.Errorf("crawl: update contact favorites progress: %w", err)
		}
	}
	return nil
}
