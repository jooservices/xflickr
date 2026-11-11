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
	var photosTotal, sizesReady, captioned *int64
	var refreshNeeded bool
	err := r.db.QueryRow(ctx, homeSnapshotSQL, userID).Scan(
		&snapshot.GeneratedAt,
		&snapshot.Contacts.Total,
		&snapshot.Contacts.Direct,
		&snapshot.Contacts.Discovered,
		&photosTotal,
		&sizesReady,
		&captioned,
		&snapshot.CountsComputedAt,
		&snapshot.Estimated,
		&snapshot.CountsStale,
		&refreshNeeded,
		&newPhotos,
		&starredContacts,
		&activity,
	)
	if err != nil {
		return app.Snapshot{}, fmt.Errorf("home: load snapshot: %w", err)
	}
	if photosTotal != nil && sizesReady != nil && captioned != nil {
		snapshot.Photos = app.PhotoCounts{Total: *photosTotal, SizesReady: *sizesReady, Captioned: *captioned}
	} else {
		snapshot.CountsStale = true
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
	if refreshNeeded && r.refreshQueue != nil {
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
WITH RECURSIVE refresh_interval AS (
    SELECT COALESCE(
        (SELECT (value #>> '{}')::bigint
         FROM app_settings
         WHERE key = 'home.stats_refresh_interval'),
        900
    ) AS seconds
),
home_versions AS (
    SELECT COALESCE(
        (SELECT revision FROM user_home_stats_versions WHERE user_id = $1), 0
    ) AS source_revision,
    (SELECT revision FROM user_home_shared_version WHERE id = TRUE) AS shared_revision
),
current_stats AS (
    SELECT stats.*,
           stats.invalidated_at IS NULL AS is_valid,
           stats.estimated
             OR stats.source_revision <> versions.source_revision
             OR stats.shared_revision <> versions.shared_revision AS is_estimated
    FROM user_home_stats stats
    CROSS JOIN home_versions versions
    WHERE stats.user_id = $1
),
current_counts_valid AS (
    SELECT COALESCE(bool_or(is_valid), FALSE) AS is_valid,
           COALESCE(bool_or(is_estimated), TRUE) AS is_estimated
    FROM current_stats
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
             WHEN EXISTS (
                 SELECT 1 FROM work_items item
                 WHERE item.user_id = $1
                   AND item.subject_key = contact.person_nsid
                   AND item.state = 'failed'
             ) THEN 'attention'
             WHEN EXISTS (
                 SELECT 1 FROM work_items item
                 WHERE item.user_id = $1
                   AND item.subject_key = contact.person_nsid
                   AND item.state = 'running'
             ) THEN 'crawling'
             WHEN EXISTS (
                 SELECT 1 FROM work_items item
                 WHERE item.user_id = $1
                   AND item.subject_key = contact.person_nsid
                   AND item.state IN ('pending', 'ready', 'waiting')
             ) THEN 'waiting'
             ELSE 'idle'
           END AS crawl_state
    FROM starred_contacts contact
    CROSS JOIN current_counts_valid valid
    LEFT JOIN user_home_contact_stats counts
      ON counts.user_id = $1 AND counts.person_nsid = contact.person_nsid AND valid.is_valid
),
shared_photo_candidates AS MATERIALIZED (
    SELECT shared_candidate.photo_id, shared_candidate.uploaded_at
    FROM user_home_shared_photo_candidates shared_candidate
    JOIN photos photo ON photo.id = shared_candidate.photo_id
    WHERE photo.visibility = 'public'
      AND photo.tombstoned_at IS NULL
      AND EXISTS (
          SELECT 1
          FROM observations observation
          JOIN sharing_settings setting
            ON setting.user_id = observation.user_id AND setting.enabled
          WHERE observation.record_kind = 'photo'
            AND observation.record_id = shared_candidate.photo_id::text
      )
    ORDER BY shared_candidate.uploaded_at DESC NULLS LAST, shared_candidate.photo_id DESC
    LIMIT 12
),
own_photo_candidates AS MATERIALIZED (
    SELECT candidate.photo_id, candidate.uploaded_at
    FROM user_home_photo_candidates candidate
    JOIN photos photo ON photo.id = candidate.photo_id
    WHERE candidate.user_id = $1
      AND photo.visibility = 'public'
      AND photo.tombstoned_at IS NULL
      AND EXISTS (
          SELECT 1
          FROM observations observation
          WHERE observation.user_id = $1
            AND observation.record_kind = 'photo'
            AND observation.record_id = candidate.photo_id::text
      )
    ORDER BY candidate.uploaded_at DESC NULLS LAST, candidate.photo_id DESC
    LIMIT 12
),
newest_photo_candidates AS MATERIALIZED (
    SELECT photo_id, uploaded_at FROM own_photo_candidates
    UNION
    SELECT photo_id, uploaded_at FROM shared_photo_candidates
    ORDER BY uploaded_at DESC NULLS LAST, photo_id DESC
    LIMIT 12
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
    FROM newest_photo_candidates candidate
    JOIN photos photo ON photo.id = candidate.photo_id
    LEFT JOIN people owner ON owner.nsid = photo.owner_nsid
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
),
work_activity AS (
    SELECT 'crawl:' || item.id::text AS id,
           'crawl'::text AS kind,
           'unknown'::text AS message_key,
           0::bigint AS event_count,
           COALESCE(NULLIF(person.realname, ''), NULLIF(person.username, ''), item.subject_key) AS contact_name,
           item.updated_at AS occurred_at,
           CASE WHEN item.state = 'failed' THEN 'error' ELSE 'info' END AS severity
    FROM work_items item
    LEFT JOIN people person ON person.nsid = item.subject_key
    WHERE item.user_id = $1
      AND item.kind IN ('photos', 'sizes')
      AND item.state IN ('ready', 'running', 'waiting', 'failed', 'done')
    ORDER BY item.updated_at DESC, item.id DESC
    LIMIT 5
),
download_activity AS (
    SELECT 'download:' || download.id::text AS id,
           'download'::text AS kind,
           'unknown'::text AS message_key,
           (SELECT count(*) FROM download_items item WHERE item.download_id = download.id) AS event_count,
           ''::text AS contact_name,
           download.created_at AS occurred_at,
           CASE WHEN download.status = 'failed' THEN 'error' ELSE 'info' END AS severity
    FROM downloads download
    WHERE download.user_id = $1
    ORDER BY download.created_at DESC, download.id DESC
    LIMIT 5
),
quota_activity AS (
    SELECT 'quota:' || call.id::text AS id,
           'quota'::text AS kind,
           'quota_updated'::text AS message_key,
           0::bigint AS event_count,
           ''::text AS contact_name,
           call.called_at AS occurred_at,
           'info'::text AS severity
    FROM api_keys api_key
    CROSS JOIN LATERAL (
        SELECT call_log.id, call_log.called_at
        FROM api_call_log call_log
        WHERE call_log.key_id = api_key.id
          AND call_log.called_at >= statement_timestamp() - interval '30 days'
        ORDER BY call_log.called_at DESC, call_log.id DESC
        LIMIT 5
    ) call
    WHERE api_key.user_id = $1
),
activity_candidates AS MATERIALIZED (
    SELECT * FROM work_activity
    UNION ALL
    SELECT * FROM download_activity
    UNION ALL
    SELECT * FROM quota_activity
),
activity_json AS (
    SELECT COALESCE(
        jsonb_agg(
            jsonb_build_object(
                'id', activity.id,
                'kind', activity.kind,
                'message_key', activity.message_key,
                'count', activity.event_count,
                'contact_name', activity.contact_name,
                'occurred_at', activity.occurred_at,
                'severity', activity.severity
            ) ORDER BY activity.occurred_at DESC, activity.id DESC
        ), '[]'::jsonb
    ) AS items
    FROM (
        SELECT * FROM activity_candidates
        ORDER BY occurred_at DESC, id DESC
        LIMIT 5
    ) activity
)
SELECT statement_timestamp(),
       contact_counts.total,
       contact_counts.direct,
       contact_counts.discovered,
       CASE WHEN counts_valid.is_valid THEN stats.photos_total END,
       CASE WHEN counts_valid.is_valid THEN stats.sizes_ready END,
       CASE WHEN counts_valid.is_valid THEN stats.captioned END,
       CASE WHEN counts_valid.is_valid THEN stats.computed_at END,
       COALESCE(
           stats.computed_at < statement_timestamp() - make_interval(secs => refresh_interval.seconds::double precision)
           OR NOT counts_valid.is_valid
           OR counts_valid.is_estimated,
           TRUE
       ),
       NOT counts_valid.is_valid,
       COALESCE(
           stats.computed_at < statement_timestamp() - make_interval(secs => refresh_interval.seconds::double precision)
           OR NOT counts_valid.is_valid,
           TRUE
       ),
       newest_json.items,
       starred_json.items,
       activity_json.items
FROM contact_counts
CROSS JOIN refresh_interval
LEFT JOIN current_stats stats ON stats.user_id = $1
CROSS JOIN current_counts_valid counts_valid
CROSS JOIN newest_json
CROSS JOIN starred_json
CROSS JOIN activity_json`
