package postgres

import (
	"bytes"
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

const loadVerificationSQL = `
SELECT a.id, a.user_id, a.key_id, a.nsid, a.username, a.status, a.created_at,
       k.id, k.user_id, k.label, k.key_public, k.per_hour_limit, k.per_second_limit,
       k.burst, k.status, k.created_at, k.secret_sealed,
       a.token_sealed, a.secret_sealed
FROM flickr_accounts a
JOIN api_keys k ON k.id = a.key_id AND k.user_id = a.user_id
WHERE a.id = $1`

const lockVerificationSQL = `
SELECT a.user_id, a.key_id, a.nsid, a.status, k.status,
       k.key_public, a.token_sealed, a.secret_sealed, k.secret_sealed
FROM flickr_accounts a
JOIN api_keys k ON k.id = a.key_id AND k.user_id = a.user_id
WHERE a.id = $1
FOR UPDATE OF a, k`

const (
	setAccountVerificationSQL = `UPDATE flickr_accounts SET status = $1 WHERE id = $2`
	setKeyVerificationSQL     = `UPDATE api_keys SET status = $1 WHERE id = $2`
)

// LoadVerification returns only sealed credential material for one account job.
func (r *Repository) LoadVerification(ctx context.Context, accountID int64) (app.VerificationCredentials, error) {
	if err := r.ready(ctx); err != nil {
		return app.VerificationCredentials{}, err
	}
	if accountID <= 0 {
		return app.VerificationCredentials{}, app.ErrInvalidInput
	}
	var result app.VerificationCredentials
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		var accountStatus, keyStatus string
		row := tx.QueryRow(ctx, loadVerificationSQL, accountID)
		if err := row.Scan(
			&result.Account.ID, &result.Account.UserID, &result.Account.KeyID, &result.Account.NSID,
			&result.Account.Username, &accountStatus, &result.Account.CreatedAt,
			&result.Key.ID, &result.Key.UserID, &result.Key.Label, &result.Key.PublicKey,
			&result.Key.PerHourLimit, &result.Key.PerSecondLimit, &result.Key.Burst,
			&keyStatus, &result.Key.CreatedAt, &result.KeySecret, &result.Token, &result.TokenSecret,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return app.ErrNotFound
			}
			return fmt.Errorf("accounts: load verification credentials: %w", err)
		}
		result.Account.Status = domain.AccountStatus(accountStatus)
		result.Key.Status = domain.Status(keyStatus)
		return nil
	})
	if err != nil {
		return app.VerificationCredentials{}, err
	}
	return result, nil
}

// RecordVerification updates statuses and publishes reconnect events atomically.
func (r *Repository) RecordVerification(ctx context.Context, expected app.VerificationCredentials, keyStatus domain.Status, accountStatus domain.AccountStatus) error {
	if err := r.ready(ctx); err != nil {
		return err
	}
	accountID := expected.Account.ID
	if accountID <= 0 || !keyStatus.Valid() || !accountStatus.Valid() {
		return app.ErrInvalidInput
	}
	return r.unit.Do(ctx, func(tx db.Tx) error {
		state, err := lockVerificationState(ctx, tx, expected)
		if err != nil {
			return err
		}
		accountChanged, keyChanged, err := validateVerificationTransition(state, keyStatus, accountStatus)
		if err != nil {
			return err
		}
		if err := updateVerificationState(ctx, tx, state, keyStatus, accountStatus, accountChanged, keyChanged); err != nil {
			return err
		}
		return publishVerificationEvents(ctx, tx, state, keyStatus, accountStatus, accountChanged, keyChanged)
	})
}

type verificationState struct {
	AccountID     int64
	UserID        int64
	KeyID         int64
	NSID          string
	PublicKey     string
	AccountStatus domain.AccountStatus
	KeyStatus     domain.Status
	Token         crypto.Sealed
	TokenSecret   crypto.Sealed
	KeySecret     crypto.Sealed
}

func lockVerificationState(ctx context.Context, tx db.Tx, expected app.VerificationCredentials) (verificationState, error) {
	state := verificationState{AccountID: expected.Account.ID}
	var currentAccountStatus, currentKeyStatus string
	err := tx.QueryRow(ctx, lockVerificationSQL, expected.Account.ID).Scan(
		&state.UserID, &state.KeyID, &state.NSID, &currentAccountStatus, &currentKeyStatus,
		&state.PublicKey, &state.Token, &state.TokenSecret, &state.KeySecret,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return verificationState{}, app.ErrNotFound
	}
	if err != nil {
		return verificationState{}, fmt.Errorf("accounts: lock verification status: %w", err)
	}
	state.AccountStatus = domain.AccountStatus(currentAccountStatus)
	state.KeyStatus = domain.Status(currentKeyStatus)
	if !verificationSnapshotMatches(expected, state) {
		return verificationState{}, app.ErrVerificationChanged
	}
	return state, nil
}

func verificationSnapshotMatches(expected app.VerificationCredentials, state verificationState) bool {
	return expected.Account.UserID == state.UserID && expected.Account.KeyID == state.KeyID && expected.Account.NSID == state.NSID &&
		expected.Key.ID == state.KeyID && expected.Key.UserID == state.UserID && expected.Key.PublicKey == state.PublicKey &&
		expected.Account.Status == state.AccountStatus && expected.Key.Status == state.KeyStatus &&
		sealedEqual(expected.Token, state.Token) && sealedEqual(expected.TokenSecret, state.TokenSecret) && sealedEqual(expected.KeySecret, state.KeySecret)
}

func validateVerificationTransition(state verificationState, keyStatus domain.Status, accountStatus domain.AccountStatus) (bool, bool, error) {
	currentAccount := domain.Account{Status: state.AccountStatus}
	if _, err := currentAccount.WithStatus(accountStatus); err != nil {
		return false, false, err
	}
	currentKey := domain.APIKey{Status: state.KeyStatus}
	if _, err := currentKey.WithStatus(keyStatus); err != nil {
		return false, false, err
	}
	return state.AccountStatus != accountStatus, state.KeyStatus != keyStatus, nil
}

func updateVerificationState(ctx context.Context, tx db.Tx, state verificationState, keyStatus domain.Status, accountStatus domain.AccountStatus, accountChanged, keyChanged bool) error {
	if keyChanged {
		if _, err := tx.Exec(ctx, setKeyVerificationSQL, string(keyStatus), state.KeyID); err != nil {
			return fmt.Errorf("accounts: update verified key status: %w", err)
		}
	}
	if accountChanged {
		if _, err := tx.Exec(ctx, setAccountVerificationSQL, string(accountStatus), state.AccountID); err != nil {
			return fmt.Errorf("accounts: update verified account status: %w", err)
		}
	}
	return nil
}

func publishVerificationEvents(ctx context.Context, tx db.Tx, state verificationState, keyStatus domain.Status, accountStatus domain.AccountStatus, accountChanged, keyChanged bool) error {
	if accountStatus == domain.AccountRevoked && state.AccountStatus != domain.AccountRevoked {
		revoked := domain.Revoked{AccountID: state.AccountID, UserID: state.UserID, KeyID: state.KeyID, NSID: state.NSID}
		if err := outbox.PublishContext(ctx, tx, domain.RevokedTopic, strconv.FormatInt(state.AccountID, 10), revoked); err != nil {
			return err
		}
	}
	if (accountChanged || keyChanged) && (accountStatus != domain.AccountOK || keyStatus != domain.StatusValid) {
		requested := domain.NotificationRequested{AccountID: state.AccountID, UserID: state.UserID, KeyID: state.KeyID, Kind: domain.NotificationReconnect}
		return outbox.PublishContext(ctx, tx, domain.NotificationRequestedTopic, strconv.FormatInt(state.AccountID, 10), requested)
	}
	return nil
}

// VerificationAccountIDs lists account identifiers only; queue args contain no credentials.
func (r *Repository) VerificationAccountIDs(ctx context.Context) ([]int64, error) {
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	ids := make([]int64, 0)
	err := r.unit.Do(ctx, func(tx db.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM flickr_accounts ORDER BY id`)
		if err != nil {
			return fmt.Errorf("accounts: list verification account ids: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("accounts: scan verification account id: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("accounts: read verification account ids: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

var _ app.VerificationRepository = (*Repository)(nil)

func sealedEqual(left, right crypto.Sealed) bool {
	return left.KeyID == right.KeyID && bytes.Equal(left.Nonce, right.Nonce) && bytes.Equal(left.Ciphertext, right.Ciphertext) && bytes.Equal(left.WrappedDEK, right.WrappedDEK)
}
