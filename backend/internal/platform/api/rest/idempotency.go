package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"
)

const IdempotencyRetention = 24 * time.Hour

type StoredResponse struct {
	Status  int         `json:"status"`
	Headers http.Header `json:"headers"`
	Body    []byte      `json:"body"`
}

func (s StoredResponse) Write(w http.ResponseWriter) error {
	for name, values := range s.Headers {
		w.Header()[name] = append([]string(nil), values...)
	}
	w.WriteHeader(s.Status)
	_, err := w.Write(s.Body)
	return err
}

type IdempotencyRecord struct {
	Principal   string
	Key         string
	Fingerprint string
	Response    *StoredResponse
	ExpiresAt   time.Time
}

// IdempotencyStore atomically claims absent/expired keys and durably saves
// results. A pending claim is never executed again while it remains valid.
type IdempotencyStore interface {
	Claim(context.Context, IdempotencyRecord) (record IdempotencyRecord, claimed bool, err error)
	Complete(context.Context, IdempotencyRecord) error
}

// Fingerprint includes the operation and media type as well as exact body bytes.
// Reformatting JSON changes the request; clients must resend identical bytes.
func Fingerprint(method, target, media string, body []byte) string {
	hash := sha256.New()
	for _, part := range []string{method, target, media} {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	_, _ = hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}

// Idempotent replays the complete first result. Errors after a claim leave it
// pending: retrying uncertain work is unsafe. The caller can reconcile it, but
// this foundation deliberately does not implement automatic retries.
func Idempotent(ctx context.Context, store IdempotencyStore, principal, key, fingerprint string, now time.Time, execute func(context.Context) (StoredResponse, error)) (StoredResponse, error) {
	if key == "" {
		return execute(ctx)
	}
	if principal == "" || len(key) > 255 || fingerprint == "" {
		return StoredResponse{}, NewProblem("validation-error", "Invalid idempotency identity or key", nil)
	}
	record, claimed, err := store.Claim(ctx, IdempotencyRecord{Principal: principal, Key: key, Fingerprint: fingerprint, ExpiresAt: now.Add(IdempotencyRetention)})
	if err != nil {
		return StoredResponse{}, err
	}
	if !claimed {
		if record.Fingerprint != fingerprint {
			return StoredResponse{}, NewProblem("conflict", "Idempotency-Key was already used for a different request", nil)
		}
		if record.Response == nil {
			return StoredResponse{}, NewProblem("conflict", "The original request is still pending", nil)
		}
		return *record.Response, nil
	}
	result, err := execute(ctx)
	if err != nil {
		return StoredResponse{}, err
	}
	record.Response = &result
	if err := store.Complete(ctx, record); err != nil {
		return StoredResponse{}, err
	}
	return result, nil
}
