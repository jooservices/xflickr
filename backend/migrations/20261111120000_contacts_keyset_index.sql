-- +goose NO TRANSACTION
-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS contacts_contact_nsid_person_nsid_idx;
CREATE INDEX CONCURRENTLY contacts_contact_nsid_person_nsid_idx ON contacts (contact_nsid, person_nsid);
CREATE INDEX CONCURRENTLY flickr_accounts_contacts_nsid_user_id_idx
    ON flickr_accounts (nsid, user_id) WHERE status='ok';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS contacts_contact_nsid_person_nsid_idx;
DROP INDEX CONCURRENTLY IF EXISTS flickr_accounts_contacts_nsid_user_id_idx;
