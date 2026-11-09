-- Synthetic query-plan measurement for the bounded Home candidate scans.
-- Run only against this ticket's worktree database:
--   psql --set=home_user_id=1 --set=home_after_photo_id=0 --set=home_batch_size=250 --file=docker/scripts/home-snapshot-plan.sql
-- The transaction creates 250,000 own and 250,000 shared synthetic candidates,
-- runs EXPLAIN ANALYZE, and rolls back without changing persistent data.

BEGIN;
CREATE TEMP TABLE user_home_photo_candidates (
    user_id BIGINT NOT NULL,
    photo_id BIGINT NOT NULL,
    uploaded_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, photo_id)
) ON COMMIT DROP;
CREATE INDEX user_home_photo_candidates_recent_idx
    ON user_home_photo_candidates (user_id, uploaded_at DESC NULLS LAST, photo_id DESC);

CREATE TEMP TABLE user_home_shared_photo_candidates (
    photo_id BIGINT PRIMARY KEY,
    uploaded_at TIMESTAMPTZ
) ON COMMIT DROP;
CREATE INDEX user_home_shared_photo_candidates_recent_idx
    ON user_home_shared_photo_candidates (uploaded_at DESC NULLS LAST, photo_id DESC);

CREATE TEMP TABLE photos (
    id BIGINT PRIMARY KEY,
    visibility TEXT NOT NULL,
    tombstoned_at TIMESTAMPTZ
) ON COMMIT DROP;

INSERT INTO user_home_photo_candidates (user_id, photo_id, uploaded_at)
SELECT :'home_user_id'::bigint, candidate_id,
       statement_timestamp() - candidate_id * interval '1 second'
FROM generate_series(1, 250000) AS candidate_id;
INSERT INTO user_home_shared_photo_candidates (photo_id, uploaded_at)
SELECT candidate_id,
       statement_timestamp() - candidate_id * interval '1 second'
FROM generate_series(250001, 500000) AS candidate_id;
INSERT INTO photos (id, visibility)
SELECT candidate_id, 'public'
FROM generate_series(1, 500000) AS candidate_id;
ANALYZE user_home_photo_candidates;
ANALYZE user_home_shared_photo_candidates;
ANALYZE photos;

EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT)
WITH own_candidates AS MATERIALIZED (
    SELECT candidate.photo_id, candidate.uploaded_at
    FROM user_home_photo_candidates candidate
    WHERE candidate.user_id = :'home_user_id'::bigint
    ORDER BY candidate.uploaded_at DESC NULLS LAST, candidate.photo_id DESC
    LIMIT 12
),
shared_candidates AS MATERIALIZED (
    SELECT candidate.photo_id, candidate.uploaded_at
    FROM user_home_shared_photo_candidates candidate
    ORDER BY candidate.uploaded_at DESC NULLS LAST, candidate.photo_id DESC
    LIMIT 12
),
candidates AS MATERIALIZED (
    SELECT photo_id, uploaded_at FROM own_candidates
    UNION
    SELECT photo_id, uploaded_at FROM shared_candidates
    ORDER BY uploaded_at DESC NULLS LAST, photo_id DESC
    LIMIT 12
)
SELECT photo.id
FROM candidates
JOIN photos photo ON photo.id = candidates.photo_id
WHERE photo.visibility = 'public' AND photo.tombstoned_at IS NULL;

EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT TEXT)
WITH candidate_pages AS MATERIALIZED (
    (SELECT candidate.photo_id
     FROM user_home_photo_candidates candidate
     WHERE candidate.user_id = :'home_user_id'::bigint
       AND candidate.photo_id > :'home_after_photo_id'::bigint
     ORDER BY candidate.photo_id
     LIMIT :home_batch_size)
    UNION
    (SELECT shared_candidate.photo_id
     FROM user_home_shared_photo_candidates shared_candidate
     WHERE shared_candidate.photo_id > :'home_after_photo_id'::bigint
     ORDER BY shared_candidate.photo_id
     LIMIT :home_batch_size)
),
page AS MATERIALIZED (
    SELECT candidate_pages.photo_id
    FROM candidate_pages
    ORDER BY candidate_pages.photo_id
    LIMIT :home_batch_size
)
SELECT page.photo_id
FROM page
LEFT JOIN photos photo
  ON photo.id = page.photo_id
 AND photo.visibility = 'public'
 AND photo.tombstoned_at IS NULL
ORDER BY page.photo_id;

ROLLBACK;
