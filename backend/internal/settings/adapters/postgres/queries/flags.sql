-- name: ListFeatureFlags :many
SELECT name, enabled, audience, user_ids
FROM feature_flags
ORDER BY name;

-- name: GetFeatureFlag :one
SELECT name, enabled, audience, user_ids
FROM feature_flags
WHERE name = $1;

-- name: UserIsAdmin :one
SELECT EXISTS (
    SELECT 1
    FROM users
    WHERE id = $1 AND role = 'admin' AND disabled_at IS NULL
);

-- name: UpsertFeatureFlag :exec
INSERT INTO feature_flags (name, enabled, audience, user_ids)
VALUES ($1, $2, $3, $4)
ON CONFLICT (name) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    audience = EXCLUDED.audience,
    user_ids = EXCLUDED.user_ids,
    updated_at = now();
