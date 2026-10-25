package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
)

type Repository struct{ Pool *pgxpool.Pool }

var _ catalogapp.DownloadPhotoRepository = Repository{}

func (r Repository) DownloadCandidates(ctx context.Context, kind, subject string, ids []int64) ([]catalogapp.DownloadCandidate, error) {
	if r.Pool == nil || ctx == nil {
		return nil, errors.New("catalog: download database and context are required")
	}
	var rows pgx.Rows
	var err error
	switch kind {
	case "contact":
		rows, err = r.Pool.Query(ctx, `WITH selected AS (SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update AS sizes_confirmed FROM photos p WHERE p.owner_nsid=$1 AND p.tombstoned_at IS NULL ORDER BY p.id LIMIT 501) SELECT p.id,p.owner_nsid,p.visibility,p.sizes_confirmed,o.user_id,COALESCE(ss.enabled,false) FROM selected p LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id ORDER BY p.id`, subject)
	case "album":
		rows, err = r.Pool.Query(ctx, `WITH selected AS (SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update AS sizes_confirmed,ap.position FROM album_photos ap JOIN photos p ON p.id=ap.photo_id WHERE ap.album_id=$1 AND p.tombstoned_at IS NULL ORDER BY ap.position,p.id LIMIT 501) SELECT p.id,p.owner_nsid,p.visibility,p.sizes_confirmed,o.user_id,COALESCE(ss.enabled,false) FROM selected p LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id ORDER BY p.position,p.id`, subject)
	case "tray":
		rows, err = r.Pool.Query(ctx, `SELECT p.id,p.owner_nsid,p.visibility,p.sizes_status='confirmed' AND p.sizes_checked_at>=p.last_update, o.user_id, COALESCE(ss.enabled,false) FROM photos p LEFT JOIN observations o ON o.record_kind='photo' AND o.record_id=p.id::text LEFT JOIN sharing_settings ss ON ss.user_id=o.user_id WHERE p.id=ANY($1::bigint[]) AND p.tombstoned_at IS NULL ORDER BY p.id`, ids)
	default:
		return nil, errors.New("catalog: invalid download kind")
	}
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
