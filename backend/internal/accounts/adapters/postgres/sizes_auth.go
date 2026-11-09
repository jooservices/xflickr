package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/accounts/domain"
)

// SizesAuthCandidate is one connected account that may sign a restricted
// photos.getSizes call. It carries no token material.
type SizesAuthCandidate struct {
	AccountID int64
	UserID    int64
	KeyID     int64
	NSID      string
}

type sizesAuthDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// SizesAuthCandidates lists ok connected accounts for a restricted sizes retry:
// the photo owner's account first, then other accounts owned by the requesting
// user. Tokens stay sealed in the database.
type SizesAuthCandidates struct{ DB sizesAuthDB }

const sizesAuthCandidatesSQL = `
SELECT a.id, a.user_id, a.key_id, a.nsid
FROM flickr_accounts a
JOIN api_keys k ON k.id = a.key_id AND k.user_id = a.user_id
WHERE a.status = '` + string(domain.AccountOK) + `'
  AND k.status = '` + string(domain.StatusValid) + `'
  AND a.user_id = $2
ORDER BY (a.nsid = $1) DESC, a.id ASC`

// List returns ordered candidates for ownerNSID and requestingUserID.
func (a SizesAuthCandidates) List(ctx context.Context, ownerNSID string, requestingUserID int64) ([]SizesAuthCandidate, error) {
	if a.DB == nil || ctx == nil || ownerNSID == "" || requestingUserID <= 0 {
		return nil, fmt.Errorf("accounts: sizes auth candidates need a database, owner, and user")
	}
	rows, err := a.DB.Query(ctx, sizesAuthCandidatesSQL, ownerNSID, requestingUserID)
	if err != nil {
		return nil, fmt.Errorf("accounts: list sizes auth candidates: %w", err)
	}
	defer rows.Close()
	out := make([]SizesAuthCandidate, 0)
	for rows.Next() {
		var candidate SizesAuthCandidate
		if err := rows.Scan(&candidate.AccountID, &candidate.UserID, &candidate.KeyID, &candidate.NSID); err != nil {
			return nil, fmt.Errorf("accounts: scan sizes auth candidate: %w", err)
		}
		out = append(out, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("accounts: iterate sizes auth candidates: %w", err)
	}
	return out, nil
}
