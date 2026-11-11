package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/app"
)

type contactCursor struct {
	Kind   string    `json:"kind"`
	ID     string    `json:"id"`
	At     time.Time `json:"at,omitempty"`
	Value  string    `json:"value,omitempty"`
	NullAt bool      `json:"null_at,omitempty"`
}

const contactPriorityBatchSize = 500

func contactPhotoVisibility(photoAlias string) string {
	return `(EXISTS(SELECT 1 FROM flickr_accounts fa CROSS JOIN LATERAL (SELECT 1 FROM observations o WHERE o.user_id=fa.user_id AND o.flickr_account_id=fa.id AND o.record_kind='photo' AND o.record_id=` + photoAlias + `.id::text LIMIT 1) seen WHERE fa.user_id=$1) OR (` + photoAlias + `.visibility='public' AND EXISTS(SELECT 1 FROM sharing_settings ss JOIN flickr_accounts fa ON fa.user_id=ss.user_id CROSS JOIN LATERAL (SELECT 1 FROM observations o WHERE o.user_id=ss.user_id AND o.flickr_account_id=fa.id AND o.record_kind='photo' AND o.record_id=` + photoAlias + `.id::text LIMIT 1) seen WHERE ss.enabled)))`
}

func encodeContactCursor(value contactCursor) (*string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("catalog: encode contact cursor: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	return &encoded, nil
}

func decodeContactCursor(raw, kind string) (*contactCursor, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, app.ErrInvalidCursor
	}
	var cursor contactCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Kind != kind || cursor.ID == "" {
		return nil, app.ErrInvalidCursor
	}
	return &cursor, nil
}

func contactPageLimit(value int) (int, error) {
	if value < 1 || value > app.MaxPageSize {
		return 0, app.ErrInvalidPage
	}
	return value, nil
}

func contactReadAvailable(ctx context.Context, r *Repository, userID int64) bool {
	return r != nil && r.db != nil && ctx != nil && userID > 0
}

func contactPageCursorBoundary(cursor *contactCursor, args []any, dateColumn, idColumn string) (string, []any, error) {
	if cursor == nil {
		return "", args, nil
	}
	id, err := strconv.ParseInt(cursor.ID, 10, 64)
	if err != nil || cursor.At.IsZero() {
		return "", nil, app.ErrInvalidCursor
	}
	args = append(args, cursor.At, id)
	return fmt.Sprintf("(%s,%s)<($%d,$%d)", dateColumn, idColumn, len(args)-1, len(args)), args, nil
}

func contactInterestCursorCondition(sortBy string, cursor *contactCursor, args []any) (string, []any, error) {
	if cursor == nil {
		return "", args, nil
	}
	switch sortBy {
	case "name":
		args = append(args, cursor.Value, cursor.ID)
		return fmt.Sprintf("(COALESCE(NULLIF(p.realname,''),p.username),ci.person_nsid)>($%d,$%d)", len(args)-1, len(args)), args, nil
	case "photos":
		photoCount, err := strconv.ParseInt(cursor.Value, 10, 64)
		if err != nil {
			return "", nil, app.ErrInvalidCursor
		}
		args = append(args, photoCount, cursor.ID)
		return fmt.Sprintf("(p.photos_count<$%d OR (p.photos_count=$%d AND ci.person_nsid>$%d))", len(args)-1, len(args)-1, len(args)), args, nil
	default:
		args = append(args, cursor.At, cursor.ID)
		return fmt.Sprintf("(COALESCE(ci.last_checked_at,ci.last_reconciled_at,ci.created_at)<$%d OR (COALESCE(ci.last_checked_at,ci.last_reconciled_at,ci.created_at)=$%d AND ci.person_nsid>$%d))", len(args)-1, len(args)-1, len(args)), args, nil
	}
}

func contactInterestConditions(userID int64, filter, query, sortBy string, cursor *contactCursor) ([]string, []any, error) {
	conditions := []string{"ci.user_id=$1"}
	args := []any{userID}
	switch filter {
	case "starred":
		conditions = append(conditions, "ci.starred")
	case "crawling":
		conditions = append(conditions, `EXISTS (SELECT 1 FROM work_items wi WHERE wi.user_id=ci.user_id AND wi.subject_key=ci.person_nsid AND wi.state IN ('pending','ready','running','waiting'))`)
	case "needs_attention":
		conditions = append(conditions, `(ci.paused_at IS NOT NULL OR EXISTS (SELECT 1 FROM work_items wi WHERE wi.user_id=ci.user_id AND wi.subject_key=ci.person_nsid AND wi.state='failed'))`)
	}
	if query != "" {
		args = append(args, "%"+query+"%")
		conditions = append(conditions, fmt.Sprintf("(p.username ILIKE $%d OR p.realname ILIKE $%d OR p.nsid ILIKE $%d)", len(args), len(args), len(args)))
	}
	cursorCondition, args, err := contactInterestCursorCondition(sortBy, cursor, args)
	if err != nil {
		return nil, nil, err
	}
	if cursorCondition != "" {
		conditions = append(conditions, cursorCondition)
	}
	return conditions, args, nil
}

func contactInterestOrder(sortBy string) string {
	switch sortBy {
	case "name":
		return `COALESCE(NULLIF(p.realname,''),p.username) ASC,ci.person_nsid ASC`
	case "photos":
		return `p.photos_count DESC,ci.person_nsid ASC`
	default:
		return `COALESCE(ci.last_checked_at,ci.last_reconciled_at,ci.created_at) DESC,ci.person_nsid ASC`
	}
}

func scanContactInterestRows(rows pgx.Rows, limit int) ([]app.ContactInterest, error) {
	items := make([]app.ContactInterest, 0, limit+1)
	for rows.Next() {
		var item app.ContactInterest
		var icon string
		var photosTotal int64
		var lastChecked, updated *time.Time
		var recent []byte
		if err := rows.Scan(&item.NSID, &item.Depth, &item.Starred, &item.PhotosCrawled, &item.FavoritesCount, &item.SizesConfirmed, &lastChecked, &item.Username, &item.Name, &icon, &photosTotal, &item.Status, &recent, &updated); err != nil {
			return nil, fmt.Errorf("catalog: scan contact interest: %w", err)
		}
		if item.Name == "" {
			item.Name = item.Username
		}
		item.PhotosTotal = photosTotal
		item.LastCheckAt = lastChecked
		item.UpdatedAt = updated
		if icon != "" {
			item.AvatarURL = &icon
		}
		if item.PhotosCrawled > 0 {
			item.SizesPercent = int(min(int64(100), item.SizesConfirmed*100/item.PhotosCrawled))
		}
		if err := json.Unmarshal(recent, &item.RecentPhotos); err != nil {
			return nil, fmt.Errorf("catalog: decode contact thumbnails: %w", err)
		}
		if item.RecentPhotos == nil {
			item.RecentPhotos = []app.ContactRecentPhoto{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: read contact interests: %w", err)
	}
	return items, nil
}

func contactInterestCounts(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID int64,
) (app.ContactInterestCounts, error) {
	const query = `SELECT count(*),count(*) FILTER(WHERE ci.starred),count(*) FILTER(WHERE EXISTS (SELECT 1 FROM work_items wi WHERE wi.user_id=ci.user_id AND wi.subject_key=ci.person_nsid AND wi.state IN ('pending','ready','running','waiting'))),count(*) FILTER(WHERE ci.paused_at IS NOT NULL OR EXISTS (SELECT 1 FROM work_items wi WHERE wi.user_id=ci.user_id AND wi.subject_key=ci.person_nsid AND wi.state='failed')) FROM crawl_interests ci WHERE ci.user_id=$1`
	var counts app.ContactInterestCounts
	if err := db.QueryRow(ctx, query, userID).Scan(&counts.All, &counts.Starred, &counts.Crawling, &counts.NeedsAttention); err != nil {
		return app.ContactInterestCounts{}, fmt.Errorf("catalog: count contact interests: %w", err)
	}
	return counts, nil
}

func setContactInterestNextCursor(page *app.ContactInterestPage, limit int, sortBy string) error {
	if len(page.Items) <= limit {
		return nil
	}
	last := page.Items[limit-1]
	value := last.Name
	if sortBy == "photos" {
		value = strconv.FormatInt(last.PhotosTotal, 10)
	}
	at := time.Time{}
	if last.UpdatedAt != nil {
		at = *last.UpdatedAt
	}
	cursor, err := encodeContactCursor(contactCursor{Kind: "interests:" + sortBy, ID: last.NSID, At: at, Value: value})
	if err != nil {
		return err
	}
	page.Items = page.Items[:limit]
	page.NextCursor = cursor
	return nil
}

func (r *Repository) ListContactInterests(ctx context.Context, userID int64, filter, query, sortBy, rawCursor string, limit int) (app.ContactInterestPage, error) {
	if !contactReadAvailable(ctx, r, userID) {
		return app.ContactInterestPage{}, app.ErrReadUnavailable
	}
	if _, err := contactPageLimit(limit); err != nil {
		return app.ContactInterestPage{}, err
	}
	cursor, err := decodeContactCursor(rawCursor, "interests:"+sortBy)
	if err != nil {
		return app.ContactInterestPage{}, err
	}
	conditions, args, err := contactInterestConditions(userID, filter, query, sortBy, cursor)
	if err != nil {
		return app.ContactInterestPage{}, err
	}
	args = append(args, limit+1)
	querySQL := `SELECT ci.person_nsid,ci.depth,ci.starred,ci.photos_crawled,ci.favorites_count,ci.sizes_completed,ci.last_checked_at,
 p.username,p.realname,p.icon,p.photos_count,
	CASE WHEN ci.paused_at IS NOT NULL THEN 'paused' ELSE COALESCE((SELECT CASE WHEN wi.state='failed' THEN 'error' WHEN wi.kind='check' AND wi.state IN ('pending','ready','running','waiting') THEN 'checking' WHEN wi.state='running' THEN 'crawling' WHEN wi.state IN ('pending','ready','waiting') THEN 'queued' ELSE 'done' END FROM work_items wi WHERE wi.user_id=ci.user_id AND wi.subject_key=ci.person_nsid ORDER BY wi.updated_at DESC,wi.id DESC LIMIT 1),'done') END,
	 (SELECT COALESCE(jsonb_agg(jsonb_build_object('id',recent.id::text,'alt',recent.title,'thumbnail_url',NULLIF(recent.sizes->0->>'url','')) ORDER BY recent.last_update DESC,recent.id DESC),'[]'::jsonb) FROM (SELECT ph.id,ph.title,ph.sizes,ph.last_update FROM photos ph WHERE ph.owner_nsid=ci.person_nsid AND ph.tombstoned_at IS NULL AND (EXISTS(SELECT 1 FROM observations o WHERE o.user_id=ci.user_id AND o.record_kind='photo' AND o.record_id=ph.id::text) OR (ph.visibility='public' AND EXISTS(SELECT 1 FROM observations o JOIN sharing_settings ss ON ss.user_id=o.user_id WHERE o.record_kind='photo' AND o.record_id=ph.id::text AND ss.enabled))) ORDER BY ph.last_update DESC,ph.id DESC LIMIT 3) recent),
 COALESCE(ci.last_checked_at,ci.last_reconciled_at,ci.created_at)
 FROM crawl_interests ci JOIN people p ON p.nsid=ci.person_nsid WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY ` + contactInterestOrder(sortBy) + ` LIMIT $` + strconv.Itoa(len(args))
	rows, err := r.db.Query(ctx, querySQL, args...)
	if err != nil {
		return app.ContactInterestPage{}, fmt.Errorf("catalog: list contact interests: %w", err)
	}
	defer rows.Close()
	items, err := scanContactInterestRows(rows, limit)
	if err != nil {
		return app.ContactInterestPage{}, err
	}
	counts, err := contactInterestCounts(ctx, r.db, userID)
	if err != nil {
		return app.ContactInterestPage{}, err
	}
	page := app.ContactInterestPage{Items: items, Counts: counts}
	if err := setContactInterestNextCursor(&page, limit, sortBy); err != nil {
		return app.ContactInterestPage{}, err
	}
	return page, nil
}

func (r *Repository) GetContact(ctx context.Context, userID int64, nsid string) (app.ContactDetail, bool, error) {
	if !contactReadAvailable(ctx, r, userID) || strings.TrimSpace(nsid) == "" {
		return app.ContactDetail{}, false, app.ErrReadUnavailable
	}
	const query = `SELECT ci.person_nsid,p.username,p.realname,NULLIF(p.icon,''),NULLIF(p.profile->>'location',''),ci.depth,ci.starred,ci.paused_at IS NOT NULL,
CASE WHEN ci.paused_at IS NOT NULL THEN 'paused' WHEN EXISTS(SELECT 1 FROM work_items w WHERE w.user_id=ci.user_id AND w.subject_key=ci.person_nsid AND w.state='failed') THEN 'failed' WHEN EXISTS(SELECT 1 FROM work_items w WHERE w.user_id=ci.user_id AND w.subject_key=ci.person_nsid AND w.state='running') THEN 'running' WHEN EXISTS(SELECT 1 FROM work_items w WHERE w.user_id=ci.user_id AND w.subject_key=ci.person_nsid AND w.state IN ('pending','ready','waiting')) THEN 'pending' ELSE 'complete' END,
ci.photos_crawled,p.photos_count,CASE WHEN ci.photos_crawled>0 THEN LEAST(100,(ci.sizes_completed*100/ci.photos_crawled)::int) ELSE 0 END,
 jsonb_build_array(
 jsonb_build_object('name','photos','completed',ci.photos_crawled,'total',p.photos_count,'status',CASE WHEN ci.photos_crawled>=p.photos_count AND p.photos_count>0 THEN 'complete' WHEN ci.photos_crawled>0 THEN 'partial' ELSE 'pending' END,'updated_at',ci.last_seen_at),
 jsonb_build_object('name','favorites','completed',ci.favorites_count,'total',ci.favorites_count,'status',CASE WHEN ci.last_fave_at IS NOT NULL THEN 'complete' ELSE 'pending' END,'updated_at',ci.last_fave_at),
 jsonb_build_object('name','albums','completed',(SELECT count(*) FROM albums a WHERE a.owner_nsid=ci.person_nsid),'total',(SELECT count(*) FROM albums a WHERE a.owner_nsid=ci.person_nsid),'status',CASE WHEN EXISTS(SELECT 1 FROM albums a WHERE a.owner_nsid=ci.person_nsid) THEN 'complete' ELSE 'pending' END,'updated_at',p.last_seen_at),
 jsonb_build_object('name','galleries','completed',(SELECT count(*) FROM galleries g WHERE g.owner_nsid=ci.person_nsid),'total',(SELECT count(*) FROM galleries g WHERE g.owner_nsid=ci.person_nsid),'status',CASE WHEN EXISTS(SELECT 1 FROM galleries g WHERE g.owner_nsid=ci.person_nsid) THEN 'complete' ELSE 'pending' END,'updated_at',p.last_seen_at),
 jsonb_build_object('name','groups','completed',(SELECT count(*) FROM person_groups pg WHERE pg.person_nsid=ci.person_nsid),'total',(SELECT count(*) FROM person_groups pg WHERE pg.person_nsid=ci.person_nsid),'status',CASE WHEN EXISTS(SELECT 1 FROM person_groups pg WHERE pg.person_nsid=ci.person_nsid) THEN 'complete' ELSE 'pending' END,'updated_at',p.last_seen_at)),
 p.photos_count,ci.favorites_count,(SELECT count(*) FROM albums a WHERE a.owner_nsid=ci.person_nsid),(SELECT count(*) FROM galleries g WHERE g.owner_nsid=ci.person_nsid),(SELECT count(*) FROM person_groups pg WHERE pg.person_nsid=ci.person_nsid),
 ci.last_checked_at,COALESCE(ci.last_checked_at,ci.last_reconciled_at,ci.created_at),
 CASE WHEN ci.via='' THEN '[]'::jsonb ELSE jsonb_build_array(jsonb_build_object('nsid',ci.via,'label',CASE WHEN ci.via='connected_account' THEN 'Connected account' ELSE ci.via END)) END
 FROM crawl_interests ci JOIN people p ON p.nsid=ci.person_nsid WHERE ci.user_id=$1 AND ci.person_nsid=$2`
	var result app.ContactDetail
	var icon, location *string
	var crawlStatus string
	var facetsJSON, foundJSON []byte
	var photosTotal int64
	err := r.db.QueryRow(ctx, query, userID, nsid).Scan(&result.NSID, &result.Username, &result.RealName, &icon, &location, &result.Depth, &result.Starred, &result.Paused, &crawlStatus, &result.PhotoRecordsCrawled, &photosTotal, &result.SizesCompleted, &facetsJSON, &result.Counts.Photos, &result.Counts.Favorites, &result.Counts.Albums, &result.Counts.Galleries, &result.Counts.Groups, &result.UpdatedAt, &result.UpdatedAt, &foundJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ContactDetail{}, false, nil
	}
	if err != nil {
		return app.ContactDetail{}, false, fmt.Errorf("catalog: get contact: %w", err)
	}
	if icon != nil {
		result.AvatarURL = icon
	}
	if location != nil {
		result.Location = location
	}
	result.CrawlStatus = crawlStatus
	result.PhotoRecordsTotal = &photosTotal
	if err := json.Unmarshal(facetsJSON, &result.Facets); err != nil {
		return app.ContactDetail{}, false, fmt.Errorf("catalog: decode contact facets: %w", err)
	}
	var paths []map[string]string
	if err := json.Unmarshal(foundJSON, &paths); err != nil {
		return app.ContactDetail{}, false, fmt.Errorf("catalog: decode contact discovery path: %w", err)
	}
	for _, path := range paths {
		result.FoundVia = append(result.FoundVia, app.ContactFoundVia{NSID: path["nsid"], Label: path["label"]})
	}
	if result.FoundVia == nil {
		result.FoundVia = []app.ContactFoundVia{}
	}
	if result.Facets == nil {
		result.Facets = []app.ContactFacet{}
	}
	return result, true, nil
}

func (r *Repository) ListContactPhotos(ctx context.Context, userID int64, nsid, rawCursor string, limit int) (app.ContactPhotoPage, error) {
	if !contactReadAvailable(ctx, r, userID) {
		return app.ContactPhotoPage{}, app.ErrReadUnavailable
	}
	if _, err := contactPageLimit(limit); err != nil {
		return app.ContactPhotoPage{}, err
	}
	cursor, err := decodeContactCursor(rawCursor, "photos:"+nsid)
	if err != nil {
		return app.ContactPhotoPage{}, err
	}
	args := []any{userID, nsid}
	condition := `EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2)`
	cursorCondition, args, err := contactPageCursorBoundary(cursor, args, "ph.last_update", "ph.id")
	if err != nil {
		return app.ContactPhotoPage{}, err
	}
	if cursorCondition != "" {
		condition += " AND " + cursorCondition
	}
	args = append(args, limit+1)
	filter := contactPhotoVisibility("ph")
	query := `SELECT ph.id,ph.title,NULLIF(ph.sizes->0->>'url',''),NULLIF(NULLIF(ph.sizes->0->>'width','')::int,0),NULLIF(NULLIF(ph.sizes->0->>'height','')::int,0),ph.taken_at,ph.sizes_status='confirmed' AND ph.sizes_checked_at>=ph.last_update,ph.last_update FROM photos ph WHERE ph.owner_nsid=$2 AND ph.tombstoned_at IS NULL AND ` + condition + ` AND ` + filter + ` ORDER BY ph.last_update DESC,ph.id DESC LIMIT $` + strconv.Itoa(len(args))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.ContactPhotoPage{}, fmt.Errorf("catalog: list contact photos: %w", err)
	}
	defer rows.Close()
	items := make([]app.ContactPhoto, 0, limit+1)
	var lastTime time.Time
	var lastID int64
	for rows.Next() {
		var item app.ContactPhoto
		var id int64
		var width, height *int
		var thumb *string
		var updated time.Time
		if err := rows.Scan(&id, &item.Title, &thumb, &width, &height, &item.TakenAt, &item.SizesAvailable, &updated); err != nil {
			return app.ContactPhotoPage{}, fmt.Errorf("catalog: scan contact photo: %w", err)
		}
		item.ID = strconv.FormatInt(id, 10)
		item.ThumbnailURL = thumb
		item.Width = width
		item.Height = height
		if len(items) < limit {
			lastTime, lastID = updated, id
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return app.ContactPhotoPage{}, fmt.Errorf("catalog: read contact photos: %w", err)
	}
	var total int64
	var updated *time.Time
	if err := r.db.QueryRow(ctx, `SELECT ci.photos_crawled,COALESCE(ci.last_seen_at,ci.last_reconciled_at,ci.created_at) FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2`, userID, nsid).Scan(&total, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.ContactPhotoPage{}, app.ErrNotFound
		}
		return app.ContactPhotoPage{}, fmt.Errorf("catalog: read contact photo summary: %w", err)
	}
	page := app.ContactPhotoPage{Items: items, Total: total, UpdatedAt: updated}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor, err = encodeContactCursor(contactCursor{Kind: "photos:" + nsid, ID: strconv.FormatInt(lastID, 10), At: lastTime})
		if err != nil {
			return app.ContactPhotoPage{}, err
		}
	}
	return page, nil
}

func (r *Repository) ListContactFavorites(ctx context.Context, userID int64, nsid, rawCursor string, limit int) (app.ContactFavoritePage, error) {
	if !contactReadAvailable(ctx, r, userID) {
		return app.ContactFavoritePage{}, app.ErrReadUnavailable
	}
	if _, err := contactPageLimit(limit); err != nil {
		return app.ContactFavoritePage{}, err
	}
	cursor, err := decodeContactCursor(rawCursor, "favorites:"+nsid)
	if err != nil {
		return app.ContactFavoritePage{}, err
	}
	args := []any{userID, nsid}
	condition := `EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2)`
	cursorCondition, args, err := contactPageCursorBoundary(cursor, args, "f.fave_date", "ph.id")
	if err != nil {
		return app.ContactFavoritePage{}, err
	}
	if cursorCondition != "" {
		condition += " AND " + cursorCondition
	}
	args = append(args, limit+1)
	filter := contactPhotoVisibility("ph")
	query := `SELECT ph.id,ph.title,NULLIF(ph.sizes->0->>'url',''),NULLIF(NULLIF(ph.sizes->0->>'width','')::int,0),NULLIF(NULLIF(ph.sizes->0->>'height','')::int,0),ph.taken_at,ph.sizes_status='confirmed' AND ph.sizes_checked_at>=ph.last_update,ph.owner_nsid,p.realname,NULLIF(p.icon,''),f.fave_date FROM favorites f JOIN photos ph ON ph.id=f.photo_id JOIN people p ON p.nsid=ph.owner_nsid WHERE f.person_nsid=$2 AND f.removed_at IS NULL AND ph.tombstoned_at IS NULL AND ` + condition + ` AND ` + filter + ` ORDER BY f.fave_date DESC,ph.id DESC LIMIT $` + strconv.Itoa(len(args))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.ContactFavoritePage{}, fmt.Errorf("catalog: list contact favorites: %w", err)
	}
	defer rows.Close()
	items := make([]app.ContactFavorite, 0, limit+1)
	var lastTime time.Time
	var lastID int64
	for rows.Next() {
		var item app.ContactFavorite
		var id int64
		var width, height *int
		var thumb, ownerName, ownerIcon *string
		var updated time.Time
		if err := rows.Scan(&id, &item.Title, &thumb, &width, &height, &item.TakenAt, &item.SizesAvailable, &item.OwnerNSID, &ownerName, &ownerIcon, &updated); err != nil {
			return app.ContactFavoritePage{}, fmt.Errorf("catalog: scan contact favorite: %w", err)
		}
		item.ID = strconv.FormatInt(id, 10)
		item.ThumbnailURL = thumb
		item.Width = width
		item.Height = height
		item.OwnerName = ownerName
		item.OwnerAvatarURL = ownerIcon
		if len(items) < limit {
			lastTime, lastID = updated, id
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return app.ContactFavoritePage{}, fmt.Errorf("catalog: read contact favorites: %w", err)
	}
	var total int64
	var updated *time.Time
	if err := r.db.QueryRow(ctx, `SELECT ci.favorites_count,COALESCE(ci.last_fave_at,ci.last_reconciled_at,ci.created_at) FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2`, userID, nsid).Scan(&total, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.ContactFavoritePage{}, app.ErrNotFound
		}
		return app.ContactFavoritePage{}, fmt.Errorf("catalog: read contact favorites summary: %w", err)
	}
	page := app.ContactFavoritePage{Items: items, Total: total, UpdatedAt: updated}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor, err = encodeContactCursor(contactCursor{Kind: "favorites:" + nsid, ID: strconv.FormatInt(lastID, 10), At: lastTime})
		if err != nil {
			return app.ContactFavoritePage{}, err
		}
	}
	return page, nil
}

func (r *Repository) ListContactAlbums(ctx context.Context, userID int64, nsid, rawCursor string, limit int) (app.ContactAlbumPage, error) {
	if !contactReadAvailable(ctx, r, userID) {
		return app.ContactAlbumPage{}, app.ErrReadUnavailable
	}
	if _, err := contactPageLimit(limit); err != nil {
		return app.ContactAlbumPage{}, err
	}
	cursor, err := decodeContactCursor(rawCursor, "albums:"+nsid)
	if err != nil {
		return app.ContactAlbumPage{}, err
	}
	if err := r.requireContact(ctx, userID, nsid); err != nil {
		return app.ContactAlbumPage{}, err
	}
	var total int64
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM albums WHERE owner_nsid=$1`, nsid).Scan(&total); err != nil {
		return app.ContactAlbumPage{}, fmt.Errorf("catalog: count contact albums: %w", err)
	}
	args := []any{userID, nsid}
	condition := ""
	if cursor != nil {
		args = append(args, cursor.NullAt, cursor.At, cursor.ID)
		condition = ` AND (($3 AND a.updated_at IS NULL AND a.id<$5) OR (NOT $3 AND (a.updated_at<$4 OR a.updated_at IS NULL OR (a.updated_at=$4 AND a.id<$5))))`
	}
	args = append(args, limit+1)
	query := `WITH album_page AS MATERIALIZED (
		SELECT a.id,a.title,p.username AS owner_name,a.primary_photo_id,a.photos_count,a.updated_at
		FROM albums a JOIN people p ON p.nsid=a.owner_nsid
		WHERE a.owner_nsid=$2 AND EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2)` + condition + `
		ORDER BY a.updated_at DESC NULLS LAST,a.id DESC LIMIT $` + strconv.Itoa(len(args)) + `
	), stored_counts AS (
		SELECT ap.album_id,count(*)::bigint AS stored_count
		FROM album_page page
		JOIN album_photos ap ON ap.album_id=page.id
		JOIN photos stored ON stored.id=ap.photo_id
		WHERE stored.tombstoned_at IS NULL AND ` + contactPhotoVisibility("stored") + `
		GROUP BY ap.album_id
	)
	SELECT page.id,page.title,page.owner_name,COALESCE(stored_counts.stored_count,0),page.photos_count,
	 COALESCE((SELECT jsonb_agg(NULLIF(cover.sizes->0->>'url','')) FROM photos cover WHERE cover.id=page.primary_photo_id AND cover.tombstoned_at IS NULL AND cover.sizes->0->>'url' IS NOT NULL AND ` + contactPhotoVisibility("cover") + `),'[]'::jsonb),
	 page.updated_at FROM album_page page LEFT JOIN stored_counts ON stored_counts.album_id=page.id`
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.ContactAlbumPage{}, fmt.Errorf("catalog: list contact albums: %w", err)
	}
	defer rows.Close()
	items := make([]app.ContactAlbum, 0, limit+1)
	var updated *time.Time
	var lastID string
	var lastAt time.Time
	var lastNullAt bool
	for rows.Next() {
		var item app.ContactAlbum
		var photos []byte
		var at *time.Time
		if err := rows.Scan(&item.ID, &item.Title, &item.OwnerName, &item.StoredCount, &item.TotalCount, &photos, &at); err != nil {
			return app.ContactAlbumPage{}, fmt.Errorf("catalog: scan contact album: %w", err)
		}
		if err := json.Unmarshal(photos, &item.CoverPhotoURLs); err != nil {
			return app.ContactAlbumPage{}, fmt.Errorf("catalog: decode contact album cover: %w", err)
		}
		if at != nil && (updated == nil || at.After(*updated)) {
			updated = at
		}
		if len(items) < limit {
			lastID, lastNullAt = item.ID, at == nil
			if at != nil {
				lastAt = *at
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return app.ContactAlbumPage{}, fmt.Errorf("catalog: read contact albums: %w", err)
	}
	page := app.ContactAlbumPage{Items: items, Total: total, UpdatedAt: updated}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor, err = encodeContactCursor(contactCursor{Kind: "albums:" + nsid, ID: lastID, At: lastAt, NullAt: lastNullAt})
		if err != nil {
			return app.ContactAlbumPage{}, err
		}
	}
	return page, nil
}

func (r *Repository) ListContactGalleries(ctx context.Context, userID int64, nsid, rawCursor string, limit int) (app.ContactGalleryPage, error) {
	if !contactReadAvailable(ctx, r, userID) {
		return app.ContactGalleryPage{}, app.ErrReadUnavailable
	}
	if _, err := contactPageLimit(limit); err != nil {
		return app.ContactGalleryPage{}, err
	}
	cursor, err := decodeContactCursor(rawCursor, "galleries:"+nsid)
	if err != nil {
		return app.ContactGalleryPage{}, err
	}
	if err := r.requireContact(ctx, userID, nsid); err != nil {
		return app.ContactGalleryPage{}, err
	}
	var total int64
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM galleries WHERE owner_nsid=$1`, nsid).Scan(&total); err != nil {
		return app.ContactGalleryPage{}, fmt.Errorf("catalog: count contact galleries: %w", err)
	}
	args := []any{userID, nsid}
	condition := ""
	if cursor != nil {
		args = append(args, cursor.NullAt, cursor.At, cursor.ID)
		condition = ` AND (($3 AND updated_at IS NULL AND id<$5) OR (NOT $3 AND (updated_at<$4 OR updated_at IS NULL OR (updated_at=$4 AND id<$5))))`
	}
	args = append(args, limit+1)
	rows, err := r.db.Query(ctx, `SELECT id,title,photos_count,updated_at FROM galleries WHERE owner_nsid=$2 AND EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2)`+condition+` ORDER BY updated_at DESC NULLS LAST,id DESC LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return app.ContactGalleryPage{}, fmt.Errorf("catalog: list contact galleries: %w", err)
	}
	defer rows.Close()
	items := make([]app.ContactGallery, 0, limit+1)
	var updated *time.Time
	var lastID string
	var lastAt time.Time
	var lastNullAt bool
	for rows.Next() {
		var item app.ContactGallery
		var at *time.Time
		if err := rows.Scan(&item.ID, &item.Title, &item.PhotoCount, &at); err != nil {
			return app.ContactGalleryPage{}, fmt.Errorf("catalog: scan contact gallery: %w", err)
		}
		if at != nil && (updated == nil || at.After(*updated)) {
			updated = at
		}
		if len(items) < limit {
			lastID, lastNullAt = item.ID, at == nil
			if at != nil {
				lastAt = *at
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return app.ContactGalleryPage{}, fmt.Errorf("catalog: read contact galleries: %w", err)
	}
	page := app.ContactGalleryPage{Items: items, Total: total, UpdatedAt: updated}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor, err = encodeContactCursor(contactCursor{Kind: "galleries:" + nsid, ID: lastID, At: lastAt, NullAt: lastNullAt})
		if err != nil {
			return app.ContactGalleryPage{}, err
		}
	}
	return page, nil
}

func (r *Repository) ListContactGroups(ctx context.Context, userID int64, nsid, rawCursor string, limit int) (app.ContactGroupPage, error) {
	if !contactReadAvailable(ctx, r, userID) {
		return app.ContactGroupPage{}, app.ErrReadUnavailable
	}
	if _, err := contactPageLimit(limit); err != nil {
		return app.ContactGroupPage{}, err
	}
	cursor, err := decodeContactCursor(rawCursor, "groups:"+nsid)
	if err != nil {
		return app.ContactGroupPage{}, err
	}
	if err := r.requireContact(ctx, userID, nsid); err != nil {
		return app.ContactGroupPage{}, err
	}
	var total int64
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM person_groups WHERE person_nsid=$1`, nsid).Scan(&total); err != nil {
		return app.ContactGroupPage{}, fmt.Errorf("catalog: count contact groups: %w", err)
	}
	args := []any{userID, nsid}
	condition := ""
	if cursor != nil {
		args = append(args, cursor.Value, cursor.ID)
		condition = ` AND (g.name>$3 OR (g.name=$3 AND g.nsid>$4))`
	}
	args = append(args, limit+1)
	rows, err := r.db.Query(ctx, `SELECT g.nsid,g.name,g.members_count,COALESCE(pref.enabled,false),g.seen_at FROM person_groups pg JOIN groups g ON g.nsid=pg.group_nsid LEFT JOIN contact_group_crawl_preferences pref ON pref.user_id=$1 AND pref.person_nsid=$2 AND pref.group_nsid=g.nsid WHERE pg.person_nsid=$2 AND EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2)`+condition+` ORDER BY g.name,g.nsid LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return app.ContactGroupPage{}, fmt.Errorf("catalog: list contact groups: %w", err)
	}
	defer rows.Close()
	items := make([]app.ContactGroup, 0, limit+1)
	var updated *time.Time
	var lastID, lastName string
	for rows.Next() {
		var item app.ContactGroup
		var at *time.Time
		if err := rows.Scan(&item.NSID, &item.Name, &item.MembersCount, &item.PoolCrawlEnabled, &at); err != nil {
			return app.ContactGroupPage{}, fmt.Errorf("catalog: scan contact group: %w", err)
		}
		if at != nil && (updated == nil || at.After(*updated)) {
			updated = at
		}
		if len(items) < limit {
			lastID, lastName = item.NSID, item.Name
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return app.ContactGroupPage{}, fmt.Errorf("catalog: read contact groups: %w", err)
	}
	page := app.ContactGroupPage{Items: items, Total: total, UpdatedAt: updated}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor, err = encodeContactCursor(contactCursor{Kind: "groups:" + nsid, ID: lastID, Value: lastName})
		if err != nil {
			return app.ContactGroupPage{}, err
		}
	}
	return page, nil
}

func (r *Repository) GetContactAbout(ctx context.Context, userID int64, nsid string) (app.ContactAbout, bool, error) {
	contact, ok, err := r.GetContact(ctx, userID, nsid)
	if err != nil || !ok {
		return app.ContactAbout{}, ok, err
	}
	var result app.ContactAbout
	var tombstones int64
	var tombstoneUpdated *time.Time
	err = r.db.QueryRow(ctx, `SELECT ci.last_checked_at,ci.tombstones_count,ci.last_reconciled_at FROM crawl_interests ci WHERE ci.user_id=$1 AND ci.person_nsid=$2`, userID, nsid).Scan(&result.LastChangeCheckAt, &tombstones, &tombstoneUpdated)
	if err != nil {
		return app.ContactAbout{}, false, fmt.Errorf("catalog: get contact about: %w", err)
	}
	result.Facets = contact.Facets
	result.TombstoneCount = tombstones
	result.TombstoneUpdatedAt = tombstoneUpdated
	result.UpdatedAt = contact.UpdatedAt
	return result, true, nil
}

func (r *Repository) SetContactGroupPoolCrawl(ctx context.Context, userID int64, nsid, group string, enabled bool) (app.ContactPoolCrawl, bool, error) {
	if !contactReadAvailable(ctx, r, userID) {
		return app.ContactPoolCrawl{}, false, app.ErrReadUnavailable
	}
	var result app.ContactPoolCrawl
	err := r.db.QueryRow(ctx, `WITH preference AS (
		INSERT INTO contact_group_crawl_preferences(user_id,person_nsid,group_nsid,enabled,changed_at)
		SELECT $1,$2,$3,$4,now()
		WHERE EXISTS(SELECT 1 FROM person_groups WHERE person_nsid=$2 AND group_nsid=$3)
		  AND EXISTS(SELECT 1 FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2)
		ON CONFLICT(user_id,person_nsid,group_nsid) DO UPDATE
		SET enabled=EXCLUDED.enabled,changed_at=EXCLUDED.changed_at
		RETURNING enabled,changed_at
	), queued AS (
		INSERT INTO work_items(kind,subject_key,user_id,key_id,depth,cursor,starred,score)
		SELECT 'group_pool',$3,$1,
			(SELECT fa.key_id FROM flickr_accounts fa WHERE fa.user_id=$1 AND fa.status='ok' ORDER BY fa.id LIMIT 1),
			ci.depth,
			jsonb_build_object('page',1,'method','flickr.groups.pools.getPhotos','params',jsonb_build_object('group_id',$3,'user_id',$2,'per_page','500','extras','url_sq,url_t,url_s,url_q,url_m,url_n,url_z,url_c,url_l,url_o,date_upload,last_update,owner_name,media,tags,description,license,o_dims,views')),
			ci.starred,
			work_items_static_score(COALESCE((SELECT p.weight FROM users u LEFT JOIN plans p ON p.id=u.plan_id WHERE u.id=$1),1),FALSE,ci.starred,ci.depth,now())
		FROM preference pref JOIN crawl_interests ci ON ci.user_id=$1 AND ci.person_nsid=$2
		WHERE pref.enabled
		ON CONFLICT ON CONSTRAINT work_items_dedupe_key DO UPDATE
		SET key_id=COALESCE(EXCLUDED.key_id,work_items.key_id),state='pending',cursor=EXCLUDED.cursor,attempts=0,last_error=NULL,
			not_before=now(),lease_expires_at=NULL,starred=EXCLUDED.starred,score=EXCLUDED.score,created_at=now(),updated_at=now()
		WHERE work_items.state IN ('done','failed')
		RETURNING id
	), canceled AS (
		UPDATE work_items wi SET state='done',last_error=NULL,lease_expires_at=NULL,updated_at=now()
		FROM preference pref
		WHERE NOT pref.enabled AND wi.kind='group_pool' AND wi.user_id=$1 AND wi.subject_key=$3
		  AND wi.state IN ('pending','ready','waiting')
		  AND NOT EXISTS(SELECT 1 FROM contact_group_crawl_preferences active WHERE active.user_id=$1 AND active.group_nsid=$3 AND active.enabled)
		RETURNING wi.id
	)
	SELECT enabled,changed_at FROM preference`, userID, nsid, group, enabled).Scan(&result.Enabled, &result.ChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ContactPoolCrawl{}, false, nil
	}
	if err != nil {
		return app.ContactPoolCrawl{}, false, fmt.Errorf("catalog: set contact group pool crawl: %w", err)
	}
	return result, true, nil
}

func (r *Repository) PrioritiseContactSizes(ctx context.Context, userID int64, nsid, rawCursor string) (app.ContactSizesPriorityPage, error) {
	if !contactReadAvailable(ctx, r, userID) {
		return app.ContactSizesPriorityPage{}, app.ErrReadUnavailable
	}
	if err := r.requireContact(ctx, userID, nsid); err != nil {
		return app.ContactSizesPriorityPage{}, err
	}
	cursor, err := decodeContactCursor(rawCursor, "sizes:"+nsid)
	if err != nil {
		return app.ContactSizesPriorityPage{}, err
	}
	var afterID int64
	if cursor != nil {
		parsedID, parseErr := strconv.ParseInt(cursor.ID, 10, 64)
		if parseErr != nil || parsedID <= 0 {
			return app.ContactSizesPriorityPage{}, app.ErrInvalidCursor
		}
		afterID = parsedID
	}
	var page app.ContactSizesPriorityPage
	var lastPhotoID int64
	var hasMore bool
	err = r.db.QueryRow(ctx, `WITH photo_page AS MATERIALIZED (
		SELECT ph.id FROM photos ph
		WHERE ph.owner_nsid=$2 AND ph.id>$3 AND ph.tombstoned_at IS NULL AND ph.sizes_status<>'confirmed'
		ORDER BY ph.id LIMIT $5
	), batch AS MATERIALIZED (
		SELECT id FROM photo_page ORDER BY id LIMIT $4
	), updated AS (
		UPDATE work_items wi SET direct_action=TRUE,score_updated_at=NULL,updated_at=now()
		FROM batch WHERE wi.user_id=$1 AND wi.subject_key=batch.id::text AND wi.kind='photo_sizes'
		  AND wi.state IN ('pending','ready','running','waiting') AND wi.direct_action=FALSE
		RETURNING wi.id
	)
	SELECT (SELECT count(*) FROM updated),
		COALESCE((SELECT max(id) FROM batch),$3),
		(SELECT count(*)>$4 FROM photo_page)`, userID, nsid, afterID, contactPriorityBatchSize, contactPriorityBatchSize+1).Scan(&page.Prioritised, &lastPhotoID, &hasMore)
	if err != nil {
		return app.ContactSizesPriorityPage{}, fmt.Errorf("catalog: prioritise contact sizes: %w", err)
	}
	if hasMore {
		page.NextCursor, err = encodeContactCursor(contactCursor{Kind: "sizes:" + nsid, ID: strconv.FormatInt(lastPhotoID, 10)})
		if err != nil {
			return app.ContactSizesPriorityPage{}, err
		}
	}
	return page, nil
}

func (r *Repository) requireContact(ctx context.Context, userID int64, nsid string) error {
	var exists bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2)`, userID, nsid).Scan(&exists); err != nil {
		return fmt.Errorf("catalog: verify contact access: %w", err)
	}
	if !exists {
		return app.ErrNotFound
	}
	return nil
}
