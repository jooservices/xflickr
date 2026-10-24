-- +goose Up
-- Older JOOXF-93 builds stored this policy separately. Preserve its value
-- while moving it into the JOOXF-86 application settings registry.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('public.auth_settings') IS NOT NULL THEN
        INSERT INTO app_settings (key, value, user_overridable)
        SELECT key, to_jsonb(value), FALSE
        FROM auth_settings
        WHERE key = 'registration_open'
        ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, user_overridable = FALSE;

        DROP TABLE auth_settings;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS auth_settings (
    key TEXT PRIMARY KEY,
    value BOOLEAN NOT NULL
);

INSERT INTO auth_settings (key, value)
SELECT key, (value #>> '{}')::boolean
FROM app_settings
WHERE key = 'registration_open'
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
-- +goose StatementEnd
