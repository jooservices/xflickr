package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
)

type Repository struct {
	Pool interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}
}

var _ catalogapp.DownloadPhotoRepository = Repository{}

func downloadCandidateRows(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, kind string, userID int64, subject string, ids []int64,
) (pgx.Rows, error) {
	switch kind {
	case "contact":
		if len(ids) > 0 {
			return pool.Query(ctx, `SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update AS sizes_confirmed,o.user_id,COALESCE(ss.enabled,false)
				FROM photos p
				LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text
				LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id
				WHERE p.owner_nsid=$2 AND p.tombstoned_at IS NULL AND p.id=ANY($3::bigint[])
				AND EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2)
				ORDER BY p.id`, userID, subject, ids)
		}
		return pool.Query(ctx, `WITH selected AS (SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update AS sizes_confirmed FROM photos p WHERE p.owner_nsid=$2 AND p.tombstoned_at IS NULL AND EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2) ORDER BY p.id LIMIT 501) SELECT p.id,p.owner_nsid,p.visibility,p.sizes_confirmed,o.user_id,COALESCE(ss.enabled,false) FROM selected p LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id ORDER BY p.id`, userID, subject)
	case "album":
		return pool.Query(ctx, `WITH selected AS (SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update AS sizes_confirmed,ap.position FROM album_photos ap JOIN photos p ON p.id=ap.photo_id WHERE ap.album_id=$1 AND p.tombstoned_at IS NULL ORDER BY ap.position,p.id LIMIT 501) SELECT p.id,p.owner_nsid,p.visibility,p.sizes_confirmed,o.user_id,COALESCE(ss.enabled,false) FROM selected p LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id ORDER BY p.position,p.id`, subject)
	case "favorites":
		if len(ids) > 0 {
			return pool.Query(ctx, `SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update AS sizes_confirmed,o.user_id,COALESCE(ss.enabled,false)
				FROM favorites f
				JOIN photos p ON p.id=f.photo_id
				LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text
				LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id
				WHERE f.person_nsid=$2 AND f.removed_at IS NULL AND p.tombstoned_at IS NULL
				AND p.id=ANY($3::bigint[])
				AND EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2)
				ORDER BY f.fave_date DESC,p.id DESC`, userID, subject, ids)
		}
		return pool.Query(ctx, `WITH selected AS (SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update AS sizes_confirmed,f.fave_date FROM favorites f JOIN photos p ON p.id=f.photo_id WHERE f.person_nsid=$2 AND f.removed_at IS NULL AND p.tombstoned_at IS NULL AND EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2) ORDER BY f.fave_date DESC,p.id DESC LIMIT 501) SELECT p.id,p.owner_nsid,p.visibility,p.sizes_confirmed,o.user_id,COALESCE(ss.enabled,false) FROM selected p LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id ORDER BY p.fave_date DESC,p.id DESC`, userID, subject)
	case "tray":
		return pool.Query(ctx, `SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update, o.user_id, COALESCE(ss.enabled,false) FROM photos p LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id WHERE p.id=ANY($1::bigint[]) AND p.tombstoned_at IS NULL ORDER BY p.id`, ids)
	default:
		return nil, errors.New("catalog: invalid download kind")
	}
}

func (r Repository) DownloadCandidates(ctx context.Context, userID int64, kind, subject string, ids []int64) ([]catalogapp.DownloadCandidate, error) {
	if r.Pool == nil || ctx == nil || userID <= 0 {
		return nil, errors.New("catalog: download database and context are required")
	}
	rows, err := downloadCandidateRows(ctx, r.Pool, kind, userID, subject, ids)
	if err != nil {
		return nil, fmt.Errorf("catalog: select download photos: %w", err)
	}
	defer rows.Close()
	byID := make(map[int64]*catalogapp.DownloadCandidate)
	order := make([]int64, 0)
	for rows.Next() {
		var id int64
		var observerID *int64
		var owner, visibility string
		var confirmed, sharing bool
		if err := rows.Scan(&id, &owner, &visibility, &confirmed, &observerID, &sharing); err != nil {
			return nil, fmt.Errorf("catalog: scan download photo: %w", err)
		}
		candidate, ok := byID[id]
		if !ok {
			candidate = &catalogapp.DownloadCandidate{PhotoID: id, OwnerNSID: owner, Visibility: visibility, SizesConfirmed: confirmed}
			byID[id] = candidate
			order = append(order, id)
		}
		if observerID != nil {
			candidate.Observers = append(candidate.Observers, catalogapp.DownloadObserver{UserID: *observerID, SharingEnabled: sharing})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: read download photos: %w", err)
	}
	result := make([]catalogapp.DownloadCandidate, 0, len(order))
	for _, id := range order {
		result = append(result, *byID[id])
	}
	return result, nil
}
