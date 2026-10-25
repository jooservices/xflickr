-- +goose Up
CREATE TABLE crawl_interests (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    person_nsid TEXT NOT NULL CHECK (char_length(person_nsid) BETWEEN 1 AND 255),
    depth INTEGER NOT NULL CHECK (depth >= 0),
    via TEXT NOT NULL CHECK (char_length(via) > 0),
    starred BOOLEAN NOT NULL DEFAULT FALSE,
    selected_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, person_nsid)
);

CREATE INDEX crawl_interests_person_nsid_idx ON crawl_interests (person_nsid);

-- +goose Down
DROP TABLE crawl_interests;
