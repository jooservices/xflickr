package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
)

func (repo *Repository) InsertSession(ctx context.Context, userID int64, tokenHash []byte, expiresAt, seenAt time.Time) (domain.Session, error) {
	var session domain.Session
	err := repo.pool.QueryRow(ctx, `
INSERT INTO sessions (user_id, token_hash, expires_at, last_seen_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, expires_at, last_seen_at, created_at`,
		userID, tokenHash, expiresAt, seenAt).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &session.LastSeenAt, &session.CreatedAt)
	if err != nil {
		return domain.Session{}, fmt.Errorf("identity: insert session: %w", err)
	}
	return session, nil
}

func (repo *Repository) FindSessionByHash(ctx context.Context, tokenHash []byte, now time.Time) (domain.Session, error) {
	var session domain.Session
	err := repo.pool.QueryRow(ctx, `
SELECT id, user_id, expires_at, last_seen_at, created_at
FROM sessions
WHERE token_hash = $1 AND expires_at > $2`, tokenHash, now).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &session.LastSeenAt, &session.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Session{}, app.ErrUnauthenticated
		}
		return domain.Session{}, fmt.Errorf("identity: find session: %w", err)
	}
	return session, nil
}

// TouchSession slides the expiry when the previous write is at least one minute old.
func (repo *Repository) TouchSession(ctx context.Context, tokenHash []byte, expiresAt, seenAt time.Time) (bool, error) {
	tag, err := repo.pool.Exec(ctx, `
UPDATE sessions
SET expires_at = $2, last_seen_at = $3
WHERE token_hash = $1
  AND expires_at > $3
  AND last_seen_at <= $3 - INTERVAL '1 minute'`, tokenHash, expiresAt, seenAt)
	if err != nil {
		return false, fmt.Errorf("identity: touch session: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (repo *Repository) DeleteOtherSessions(ctx context.Context, userID int64, tokenHash []byte) error {
	_, err := repo.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, userID, tokenHash)
	if err != nil {
		return fmt.Errorf("identity: revoke other sessions: %w", err)
	}
	return nil
}

func (repo *Repository) DeleteSessionByHash(ctx context.Context, tokenHash []byte) error {
	_, err := repo.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("identity: revoke session: %w", err)
	}
	return nil
}
