-- +goose Up
CREATE INDEX contacts_contact_nsid_person_nsid_idx ON contacts (contact_nsid, person_nsid);

-- +goose Down
DROP INDEX contacts_contact_nsid_person_nsid_idx;
