// Package postgres reads Home snapshots from the XFlickr PostgreSQL catalog.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/queue"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type refreshEnqueuer interface {
	EnqueueStandalone(context.Context, queue.Job, queue.Options) error
}

// Repository reads and refreshes user-scoped Home statistics in PostgreSQL.
type Repository struct {
	db           queryer
	refreshQueue refreshEnqueuer
}

var (
	_ app.Reader         = (*Repository)(nil)
	_ app.BatchProcessor = (*Repository)(nil)
)

// New creates a Home repository backed by a pgx database.
func New(database queryer, refreshQueues ...refreshEnqueuer) (*Repository, error) {
	if database == nil {
		return nil, errors.New("home: postgres database is required")
	}
	if len(refreshQueues) > 1 || len(refreshQueues) == 1 && refreshQueues[0] == nil {
		return nil, errors.New("home: at most one non-nil refresh queue is allowed")
	}
	repository := &Repository{db: database}
	if len(refreshQueues) == 1 {
		repository.refreshQueue = refreshQueues[0]
	}
	return repository, nil
}

// Load returns bounded lists and cached counts for one user.
func (r *Repository) Load(ctx context.Context, userID int64) (app.Snapshot, error) {
	if r == nil || r.db == nil || ctx == nil || userID <= 0 {
		return app.Snapshot{}, errors.New("home: repository, context, and user id are required")
	}
	var snapshot app.Snapshot
	var newPhotos, starredContacts, activity []byte
	err := r.db.QueryRow(ctx, homeSnapshotSQL, userID).Scan(
		&snapshot.GeneratedAt,
		&snapshot.Contacts.Total,
		&snapshot.Contacts.Direct,
		&snapshot.Contacts.Discovered,
		&snapshot.Photos.Total,
		&snapshot.Photos.SizesReady,
		&snapshot.Photos.Captioned,
		&snapshot.CountsComputedAt,
		&snapshot.Estimated,
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
	if snapshot.Estimated && r.refreshQueue != nil {
		args, err := json.Marshal(app.RefreshJob{UserID: userID})
		if err != nil {
			return app.Snapshot{}, fmt.Errorf("home: encode stats refresh job: %w", err)
		}
		if err := r.refreshQueue.EnqueueStandalone(ctx, queue.Job{Kind: app.RefreshJobKind, Args: args}, queue.Options{
			Queue: "home", MaxAttempts: 5, LeaseTimeout: time.Minute, UniqueByArgs: true, UniqueByQueue: true,
		}); err != nil {
			return app.Snapshot{}, fmt.Errorf("home: enqueue stats refresh: %w", err)
		}
	}
	return snapshot, nil
}

const homeSnapshotSQL = `
WITH refresh_interval AS (
    SELECT COALESCE(
        (SELECT (value #>> '{}')::bigint
         FROM app_settings
         WHERE key = 'home.stats_refresh_interval'),
        900
    ) AS seconds
),
contact_counts AS MATERIALIZED (
    SELECT count(*) FILTER (WHERE depth > 0)::bigint AS total,
           count(*) FILTER (WHERE depth = 1)::bigint AS direct,
           count(*) FILTER (WHERE depth > 1)::bigint AS discovered
    FROM crawl_interests
    WHERE user_id = $1
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
           COALESCE(counts.photos_total, 0)::bigint AS photos_count,
           COALESCE(counts.sizes_ready, 0)::bigint AS sizes_ready,
           COALESCE(counts.photos_total, 0)::bigint AS sizes_total,
           CASE
             WHEN COALESCE(state.failed, FALSE) THEN 'attention'
             WHEN COALESCE(state.running, FALSE) THEN 'crawling'
             WHEN COALESCE(state.queued, FALSE) THEN 'waiting'
             ELSE 'idle'
           END AS crawl_state
    FROM starred_contacts contact
    LEFT JOIN user_home_contact_stats counts
      ON counts.user_id = $1 AND counts.person_nsid = contact.person_nsid
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
    FROM photos photo
    LEFT JOIN people owner ON owner.nsid = photo.owner_nsid
    WHERE photo.visibility = 'public'
      AND photo.tombstoned_at IS NULL
      AND (
          EXISTS (
              SELECT 1
              FROM observations own_observation
              WHERE own_observation.user_id = $1
                AND own_observation.record_kind = 'photo'
                AND own_observation.record_id = photo.id::text
          )
          OR EXISTS (
              SELECT 1
              FROM observations shared_observation
              JOIN sharing_settings setting
                ON setting.user_id = shared_observation.user_id AND setting.enabled
              WHERE shared_observation.record_kind = 'photo'
                AND shared_observation.record_id = photo.id::text
          )
      )
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
       COALESCE(stats.photos_total, 0)::bigint,
       COALESCE(stats.sizes_ready, 0)::bigint,
       COALESCE(stats.captioned, 0)::bigint,
       stats.computed_at,
       COALESCE(
           stats.computed_at < statement_timestamp() - make_interval(secs => refresh_interval.seconds::double precision),
           TRUE
       ),
       newest_json.items,
       starred_json.items,
       '[]'::jsonb
FROM contact_counts
CROSS JOIN refresh_interval
LEFT JOIN user_home_stats stats ON stats.user_id = $1
CROSS JOIN newest_json
CROSS JOIN starred_json`
