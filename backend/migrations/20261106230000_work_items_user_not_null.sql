-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
    null_count bigint;
BEGIN
    SELECT count(*) INTO null_count FROM work_items WHERE user_id IS NULL;
    IF null_count > 0 THEN
        RAISE EXCEPTION 'work_items has % rows with NULL user_id; refusing to set NOT NULL', null_count;
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE work_items ALTER COLUMN user_id SET NOT NULL;

-- +goose Down
ALTER TABLE work_items ALTER COLUMN user_id DROP NOT NULL;
