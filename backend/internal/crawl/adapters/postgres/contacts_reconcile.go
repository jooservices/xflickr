package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type contactsReconcileTx struct{ tx Conn }

const contactsReconcileBatchSize = 500

const deleteStaleContactsBatchSQL = `
	WITH stale AS (
		SELECT person_nsid, contact_nsid
		FROM contacts
		WHERE person_nsid=$1 AND contact_nsid>$2 AND seen_at<$3
		ORDER BY contact_nsid
		LIMIT $4
	), deleted AS (
		DELETE FROM contacts c
		USING stale
		WHERE c.person_nsid=stale.person_nsid AND c.contact_nsid=stale.contact_nsid
		RETURNING c.contact_nsid
	)
	SELECT count(*), COALESCE(max(contact_nsid), '') FROM deleted`

func (r contactsReconcileTx) CompleteContacts(ctx context.Context, userID int64, accountNSID string, scannedAt time.Time) error {
	if userID <= 0 || accountNSID == "" || scannedAt.IsZero() {
		return errors.New("crawl: invalid contacts reconciliation")
	}
	var exists int
	err := r.tx.QueryRow(ctx, `SELECT 1 FROM flickr_accounts WHERE user_id=$1 AND nsid=$2 AND status='ok' FOR UPDATE`, userID, accountNSID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("crawl: lock contacts account: %w", err)
	}
	var cursor string
	for {
		var deleted int64
		var nextCursor string
		if err := r.tx.QueryRow(ctx, deleteStaleContactsBatchSQL, accountNSID, cursor, scannedAt, contactsReconcileBatchSize).Scan(&deleted, &nextCursor); err != nil {
			return fmt.Errorf("crawl: delete contacts missing from full list: %w", err)
		}
		if deleted == 0 {
			break
		}
		if nextCursor <= cursor {
			return errors.New("crawl: contacts reconciliation batch cursor did not advance")
		}
		cursor = nextCursor
		if deleted < contactsReconcileBatchSize {
			break
		}
	}
	return nil
}
