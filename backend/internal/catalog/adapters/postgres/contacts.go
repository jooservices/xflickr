package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Contact records a person's contact relationship.
type Contact struct {
	PersonNSID  string
	ContactNSID string
	SeenAt      time.Time
}

// UpsertContactsBatch inserts or refreshes contact relationships.
func (r *Repository) UpsertContactsBatch(ctx context.Context, contacts []Contact) error {
	if err := validateBatchSize(len(contacts)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, c := range contacts {
		if c.PersonNSID == "" || c.ContactNSID == "" || c.PersonNSID == c.ContactNSID {
			return errors.New("catalog: invalid contact")
		}
		batch.Queue(`INSERT INTO contacts (person_nsid,contact_nsid,seen_at) VALUES ($1,$2,$3) ON CONFLICT (person_nsid,contact_nsid) DO UPDATE SET seen_at=EXCLUDED.seen_at WHERE contacts.seen_at IS DISTINCT FROM EXCLUDED.seen_at`, c.PersonNSID, c.ContactNSID, seenAt(c.SeenAt))
	}
	return executeBatch(ctx, r.db, batch, "contacts")
}
