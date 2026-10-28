-- +goose Up
ALTER TABLE api_call_log ALTER COLUMN key_id DROP NOT NULL;

-- +goose Down
-- Do not discard first-time key-validation calls to restore the old constraint.
-- A rollback must wait until those rows have been explicitly handled.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM api_call_log WHERE key_id IS NULL) THEN
        RAISE EXCEPTION 'cannot restore api_call_log.key_id NOT NULL while unassociated call rows exist';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE api_call_log ALTER COLUMN key_id SET NOT NULL;
