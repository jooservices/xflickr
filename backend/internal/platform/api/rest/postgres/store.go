// Package postgres implements the REST idempotency storage port.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

type DB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Store struct{ DB DB }

func (s Store) Claim(ctx context.Context, r rest.IdempotencyRecord) (rest.IdempotencyRecord, bool, error) {
	var key string
	err := s.DB.QueryRow(ctx, `INSERT INTO rest_idempotency (principal, key, fingerprint, expires_at)
 VALUES ($1,$2,$3,$4) ON CONFLICT (principal,key) DO UPDATE
 SET fingerprint=EXCLUDED.fingerprint,response=NULL,expires_at=EXCLUDED.expires_at
 WHERE rest_idempotency.expires_at <= CURRENT_TIMESTAMP RETURNING key`, r.Principal, r.Key, r.Fingerprint, r.ExpiresAt).Scan(&key)
	if err == nil {
		return r, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return r, false, fmt.Errorf("claim idempotency key: %w", err)
	}
	var response []byte
	err = s.DB.QueryRow(ctx, `SELECT fingerprint,response,expires_at FROM rest_idempotency WHERE principal=$1 AND key=$2`, r.Principal, r.Key).Scan(&r.Fingerprint, &response, &r.ExpiresAt)
	if err != nil {
		return r, false, fmt.Errorf("read idempotency key: %w", err)
	}
	if len(response) > 0 {
		r.Response = new(rest.StoredResponse)
		if err := json.Unmarshal(response, r.Response); err != nil {
			return r, false, fmt.Errorf("decode stored response: %w", err)
		}
	}
	return r, false, nil
}

func (s Store) Complete(ctx context.Context, r rest.IdempotencyRecord) error {
	encoded, err := json.Marshal(r.Response)
	if err != nil {
		return fmt.Errorf("encode stored response: %w", err)
	}
	var key string
	// Match the claim generation, so a worker finishing after expiry cannot
	// overwrite a newer request using the same key.
	err = s.DB.QueryRow(ctx, `UPDATE rest_idempotency SET response=$5 WHERE principal=$1 AND key=$2 AND fingerprint=$3 AND expires_at=$4 AND response IS NULL RETURNING key`, r.Principal, r.Key, r.Fingerprint, r.ExpiresAt, encoded).Scan(&key)
	if err != nil {
		return fmt.Errorf("complete idempotency key: %w", err)
	}
	return nil
}
