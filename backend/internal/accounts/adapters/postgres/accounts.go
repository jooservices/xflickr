package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/outbox"
)

//nolint:gosec // G101 false positive: SQL text naming credential columns
const (
	//nolint:gosec // SQL column list names credential fields but contains no credential values.
	apiKeyRowSQL = `
SELECT id, user_id, label, key_public, per_hour_limit, per_second_limit, burst, status, created_at, secret_sealed
FROM api_keys
WHERE id = $1 AND user_id = $2`

	// lockKeySQL keeps the key from being deleted or revoked while the account is stored.
	lockKeySQL = `SELECT status FROM api_keys WHERE id = $1 AND user_id = $2 FOR SHARE`

	findAccountSQL = `
SELECT id FROM flickr_accounts
WHERE user_id = $1 AND nsid = $2
FOR UPDATE`

	insertAccountSQL = `
INSERT INTO flickr_accounts (id, user_id, key_id, nsid, username, token_sealed, secret_sealed, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING created_at`

	refreshAccountSQL = `
UPDATE flickr_accounts
SET key_id = $2, username = $3, token_sealed = $4, secret_sealed = $5, status = $6
WHERE id = $1
RETURNING created_at`

	nextAccountIDSQL = `SELECT nextval(pg_get_serial_sequence('flickr_accounts', 'id'))`

	revokeSQL = `
UPDATE flickr_accounts
SET status = '` + string(domain.AccountRevoked) + `'
WHERE id = $1 AND status <> '` + string(domain.AccountRevoked) + `'
RETURNING user_id, key_id, nsid`
)

// KeyCredentials returns the user's key and its sealed secret so the OAuth
// flow can sign requests. Only this method loads the sealed secret.
func (r *Repository) KeyCredentials(ctx context.Context, userID, keyID int64) (domain.APIKey, crypto.Sealed, error) {
	if err := r.ready(ctx); err != nil {
		return domain.APIKey{}, crypto.Sealed{}, err
	}
	var key domain.APIKey
	var sealed crypto.Sealed
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		var status string
		err := tx.QueryRow(ctx, apiKeyRowSQL, keyID, userID).Scan(
			&key.ID, &key.UserID, &key.Label, &key.PublicKey,
			&key.PerHourLimit, &key.PerSecondLimit, &key.Burst, &status, &key.CreatedAt, &sealed,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("accounts: load api key credentials: %w", err)
		}
		key.Status = domain.Status(status)
		return nil
	})
	if err != nil {
		return domain.APIKey{}, crypto.Sealed{}, err
	}
	return key, sealed, nil
}

// Connect stores the account and publishes flickr_account.connected in one
// transaction. An account the user already connected keeps its id: its key,
// username and tokens are replaced and its status becomes ok.
func (r *Repository) Connect(ctx context.Context, account domain.Account, seal func(int64) (crypto.Sealed, crypto.Sealed, error)) (domain.Account, bool, error) {
	if err := r.ready(ctx); err != nil {
		return domain.Account{}, false, err
	}
	if seal == nil {
		return domain.Account{}, false, errors.New("accounts: sealer is required")
	}
	var stored domain.Account
	var reconnected bool
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		var keyStatus string
		err := tx.QueryRow(ctx, lockKeySQL, account.KeyID, account.UserID).Scan(&keyStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.ErrKeyNotUsable
		}
		if err != nil {
			return fmt.Errorf("accounts: lock api key: %w", err)
		}
		if domain.Status(keyStatus) != domain.StatusValid {
			return app.ErrKeyNotUsable
		}

		account.Status = domain.AccountOK
		reconnected = true
		err = tx.QueryRow(ctx, findAccountSQL, account.UserID, account.NSID).Scan(&account.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			reconnected = false
			err = tx.QueryRow(ctx, nextAccountIDSQL).Scan(&account.ID)
		}
		if err != nil {
			return fmt.Errorf("accounts: allocate flickr account: %w", err)
		}
		token, secret, err := seal(account.ID)
		if err != nil {
			return err
		}
		if reconnected {
			err = tx.QueryRow(ctx, refreshAccountSQL, account.ID, account.KeyID, account.Username, token, secret, string(account.Status)).Scan(&account.CreatedAt)
		} else {
			err = tx.QueryRow(ctx, insertAccountSQL, account.ID, account.UserID, account.KeyID, account.NSID, account.Username, token, secret, string(account.Status)).Scan(&account.CreatedAt)
		}
		if err != nil {
			return fmt.Errorf("accounts: store flickr account: %w", err)
		}
		stored = account
		return outbox.PublishContext(ctx, tx, domain.ConnectedTopic, strconv.FormatInt(account.ID, 10), domain.Connected{
			AccountID:   account.ID,
			UserID:      account.UserID,
			KeyID:       account.KeyID,
			NSID:        account.NSID,
			Username:    account.Username,
			Interest:    domain.InterestPerson,
			Depth:       domain.ConnectedDepth,
			Reconnected: reconnected,
		})
	})
	if err != nil {
		return domain.Account{}, false, err
	}
	return stored, reconnected, nil
}

// MarkRevoked sets the account revoked and publishes account.revoked in one
// transaction. A missing or already revoked account changes nothing.
func (r *Repository) MarkRevoked(ctx context.Context, accountID int64) (bool, error) {
	if err := r.ready(ctx); err != nil {
		return false, err
	}
	var changed bool
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		changed = false
		var event domain.Revoked
		err := tx.QueryRow(ctx, revokeSQL, accountID).Scan(&event.UserID, &event.KeyID, &event.NSID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("accounts: revoke flickr account: %w", err)
		}
		event.AccountID = accountID
		changed = true
		return outbox.PublishContext(ctx, tx, domain.RevokedTopic, strconv.FormatInt(accountID, 10), event)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

var _ app.AccountRepository = (*Repository)(nil)
