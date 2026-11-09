-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS crawl_interests_selection_expiry_idx
    ON crawl_interests (selected_until, user_id, person_nsid)
    WHERE selected_until IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION work_items_active_direct_action(
    p_direct BOOLEAN,
    p_user_id BIGINT,
    p_person_nsid TEXT
) RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT p_direct AND COALESCE(
        (
            SELECT selected_until IS NULL OR selected_until > now()
            FROM crawl_interests
            WHERE user_id = p_user_id AND person_nsid = p_person_nsid
        ),
        TRUE
    );
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS work_items_active_direct_action(BOOLEAN, BIGINT, TEXT);
DROP INDEX CONCURRENTLY IF EXISTS crawl_interests_selection_expiry_idx;
