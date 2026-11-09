// Package postgres reads Home snapshots from the XFlickr PostgreSQL catalog.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/home/app"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Repository reads a user-scoped Home snapshot from PostgreSQL.
type Repository struct{ db queryer }

var _ app.Reader = (*Repository)(nil)

// New creates a Home repository backed by a pgx database.
func New(database queryer) (*Repository, error) {
	if database == nil {
		return nil, errors.New("home: postgres database is required")
	}
	return &Repository{db: database}, nil
}

// Load returns counts and bounded lists using the shared photo visibility rule.
func (r *Repository) Load(ctx context.Context, userID int64) (app.Snapshot, error) {
	if r == nil || r.db == nil || ctx == nil || userID <= 0 {
		return app.Snapshot{}, errors.New("home: repository, context, and user id are required")
	}
	filter := sharingapp.FilterByUserID(userID)
	query := fmt.Sprintf(
		homeSnapshotSQL,
		filter.PhotoIDs.Expression,
		"photo."+homePhotoVisibilityPredicate,
		"stored."+homePhotoVisibilityPredicate,
	)
	var snapshot app.Snapshot
	var newPhotos, starredContacts, activity []byte
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&snapshot.GeneratedAt,
		&snapshot.Contacts.Total,
		&snapshot.Contacts.Direct,
		&snapshot.Contacts.Discovered,
		&snapshot.Photos.Total,
		&snapshot.Photos.SizesReady,
		&snapshot.Photos.Captioned,
		&newPhotos,
		&starredContacts,
		&activity,
	)
	if err != nil {
		return app.Snapshot{}, fmt.Errorf("home: load snapshot: %w", err)
	}
	if err := json.Unmarshal(newPhotos, &snapshot.NewPhotos); err != nil {
		return app.Snapshot{}, fmt.Errorf("home: decode newest photos: %w", err)
	}
	if err := json.Unmarshal(starredContacts, &snapshot.StarredContacts); err != nil {
		return app.Snapshot{}, fmt.Errorf("home: decode starred contacts: %w", err)
	}
	if err := json.Unmarshal(activity, &snapshot.Activity); err != nil {
		return app.Snapshot{}, fmt.Errorf("home: decode activity: %w", err)
	}
	return snapshot, nil
}

const homeSnapshotSQL = `
WITH visible_photo_ids AS MATERIALIZED (
    %s
),
contact_counts AS MATERIALIZED (
    SELECT count(*) FILTER (WHERE depth > 0)::bigint AS total,
           count(*) FILTER (WHERE depth = 1)::bigint AS direct,
           count(*) FILTER (WHERE depth > 1)::bigint AS discovered
    FROM crawl_interests
    WHERE user_id = $1
),
visible_photo_counts AS MATERIALIZED (
    SELECT p.owner_nsid,
           count(*)::bigint AS photos_count,
           count(*) FILTER (
               WHERE p.sizes_status = 'confirmed'
                 AND p.sizes_checked_at >= p.last_update
           )::bigint AS sizes_ready,
           count(*) FILTER (
               WHERE EXISTS (
                   SELECT 1
                   FROM photo_enrichments enrichment
                   WHERE enrichment.photo_id = p.id
                     AND enrichment.kind = 'caption'
               )
           )::bigint AS captioned
    FROM visible_photo_ids visible
    CROSS JOIN LATERAL (
        SELECT photo.id, photo.owner_nsid, photo.sizes_status,
               photo.sizes_checked_at, photo.last_update
        FROM photos photo
        WHERE photo.id = visible.photo_id
          AND %s
        LIMIT 1
    ) p
    GROUP BY p.owner_nsid
),
photo_counts AS MATERIALIZED (
    SELECT COALESCE(sum(photos_count), 0)::bigint AS total,
           COALESCE(sum(sizes_ready), 0)::bigint AS sizes_ready,
           COALESCE(sum(captioned), 0)::bigint AS captioned
    FROM visible_photo_counts
),
starred_contacts AS MATERIALIZED (
    SELECT interest.person_nsid,
           COALESCE(NULLIF(person.realname, ''), NULLIF(person.username, ''), interest.person_nsid) AS name,
           person.icon
    FROM crawl_interests interest
    JOIN people person ON person.nsid = interest.person_nsid
    WHERE interest.user_id = $1 AND interest.starred
    ORDER BY interest.person_nsid
    LIMIT 5
),
starred_data AS (
    SELECT contact.person_nsid,
           contact.name,
           contact.icon,
           COALESCE(counts.photos_count, 0)::bigint AS photos_count,
           COALESCE(counts.sizes_ready, 0)::bigint AS sizes_ready,
           COALESCE(counts.photos_count, 0)::bigint AS sizes_total,
           CASE
             WHEN COALESCE(state.failed, FALSE) THEN 'attention'
             WHEN COALESCE(state.running, FALSE) THEN 'crawling'
             WHEN COALESCE(state.queued, FALSE) THEN 'waiting'
             ELSE 'idle'
           END AS crawl_state
    FROM starred_contacts contact
    LEFT JOIN visible_photo_counts counts ON counts.owner_nsid = contact.person_nsid
    LEFT JOIN LATERAL (
        SELECT bool_or(item.state = 'failed') AS failed,
               bool_or(item.state = 'running') AS running,
               bool_or(item.state IN ('pending', 'ready', 'waiting')) AS queued
        FROM work_items item
        WHERE item.user_id = $1
          AND item.subject_key = contact.person_nsid
          AND item.state IN ('pending', 'ready', 'running', 'waiting', 'failed')
    ) state ON TRUE
),
newest_photos AS (
    SELECT photo.id,
           photo.title,
           photo.owner_nsid,
           COALESCE(NULLIF(owner.realname, ''), NULLIF(owner.username, ''), photo.owner_nsid) AS owner_name,
           photo.taken_at,
           EXISTS (
               SELECT 1
               FROM photo_enrichments enrichment
               WHERE enrichment.photo_id = photo.id
                 AND enrichment.kind = 'caption'
           ) AS captioned,
           photo.uploaded_at,
           photo.sizes
    FROM visible_photo_ids visible
    CROSS JOIN LATERAL (
        SELECT stored.id, stored.title, stored.owner_nsid, stored.taken_at,
               stored.uploaded_at, stored.sizes
        FROM photos stored
        WHERE stored.id = visible.photo_id
          AND %s
        LIMIT 1
    ) photo
    LEFT JOIN people owner ON owner.nsid = photo.owner_nsid
    ORDER BY photo.uploaded_at DESC NULLS LAST, photo.id DESC
    LIMIT 12
),
newest_json AS (
    SELECT COALESCE(
        jsonb_agg(
            jsonb_build_object(
                'id', newest.id,
                'title', newest.title,
                'thumbnail_url', (
                    SELECT size.value->>'url'
                    FROM jsonb_array_elements(newest.sizes) WITH ORDINALITY AS size(value, position)
                    WHERE NULLIF(size.value->>'url', '') IS NOT NULL
                    ORDER BY CASE size.value->>'label'
                        WHEN 'Small 150' THEN 0
                        WHEN 'Square' THEN 1
                        WHEN 'Thumbnail' THEN 2
                        WHEN 'Small 240' THEN 3
                        ELSE 4
                    END, size.position
                    LIMIT 1
                ),
                'owner_nsid', newest.owner_nsid,
                'owner_name', newest.owner_name,
                'taken_at', newest.taken_at,
                'captioned', newest.captioned
            ) ORDER BY newest.uploaded_at DESC NULLS LAST, newest.id DESC
        ), '[]'::jsonb
    ) AS items
    FROM newest_photos newest
),
starred_json AS (
    SELECT COALESCE(
        jsonb_agg(
            jsonb_build_object(
                'nsid', starred.person_nsid,
                'name', starred.name,
                'icon_url', starred.icon,
                'photos_count', starred.photos_count,
                'sizes_ready', starred.sizes_ready,
                'sizes_total', starred.sizes_total,
                'crawl_state', starred.crawl_state
            ) ORDER BY starred.person_nsid
        ), '[]'::jsonb
    ) AS items
    FROM starred_data starred
)
SELECT statement_timestamp(),
       contact_counts.total,
       contact_counts.direct,
       contact_counts.discovered,
       photo_counts.total,
       photo_counts.sizes_ready,
       photo_counts.captioned,
       newest_json.items,
       starred_json.items,
       '[]'::jsonb
FROM contact_counts
CROSS JOIN photo_counts
CROSS JOIN newest_json
CROSS JOIN starred_json`

const homePhotoVisibilityPredicate = "visibility = 'public' AND tombstoned_at IS NULL"
