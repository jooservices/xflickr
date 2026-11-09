-- +goose NO TRANSACTION
-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS contacts_contact_nsid_person_nsid_idx;
CREATE INDEX CONCURRENTLY contacts_contact_nsid_person_nsid_idx ON contacts (contact_nsid, person_nsid);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS contacts_contact_nsid_person_nsid_idx;
