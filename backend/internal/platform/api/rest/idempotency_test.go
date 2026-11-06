package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIdempotencyFingerprint(t *testing.T) {
	if Fingerprint("POST", "/a", "application/json", []byte(`{}`)) == Fingerprint("POST", "/b", "application/json", []byte(`{}`)) {
		t.Fatal("operations collide")
	}
}

type memoryStore struct {
	record      IdempotencyRecord
	claimed     bool
	err         error
	completeErr error
}

func (s *memoryStore) Claim(_ context.Context, r IdempotencyRecord) (IdempotencyRecord, bool, error) {
	if s.claimed {
		return s.record, false, s.err
	}
	s.record = r
	s.claimed = true
	return r, true, s.err
}

func (s *memoryStore) Complete(_ context.Context, r IdempotencyRecord) error {
	s.record = r
	return s.completeErr
}

func TestIdempotencyCases(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	calls := 0
	execute := func(context.Context) (StoredResponse, error) {
		calls++
		return StoredResponse{Status: 202, Headers: http.Header{"Location": {"/jobs/1"}}, Body: []byte("accepted")}, nil
	}
	store := &memoryStore{}
	for range 2 {
		got, err := Idempotent(ctx, store, "user:1", "key", "fp", now, execute)
		if err != nil || got.Status != 202 {
			t.Fatal(got, err)
		}
	}
	if calls != 1 || !store.record.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatal(calls, store.record)
	}
	var problem *Problem
	if _, err := Idempotent(ctx, store, "user:1", "key", "changed", now, execute); !errors.As(err, &problem) || problem.Status != 409 {
		t.Fatal(err)
	}
	store.record.Response = nil
	if _, err := Idempotent(ctx, store, "user:1", "key", "fp", now, execute); err == nil {
		t.Fatal("pending executed")
	}
	if _, err := Idempotent(ctx, nil, "", "", "", now, execute); err != nil {
		t.Fatal(err)
	}
	if _, err := Idempotent(ctx, store, "", "key", "fp", now, execute); err == nil {
		t.Fatal("missing principal")
	}
	failure := errors.New("storage failed")
	for _, s := range []*memoryStore{{err: failure}, {completeErr: failure}} {
		if _, err := Idempotent(ctx, s, "user", "key", "fp", now, execute); !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
	if _, err := Idempotent(ctx, &memoryStore{}, "user", "key", "fp", now, func(context.Context) (StoredResponse, error) { return StoredResponse{}, failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := storeResponse().Write(w); err != nil || w.Code != 201 || w.Body.String() != "body" || w.Header().Get("Location") != "/1" {
		t.Fatal(w, err)
	}
}

func storeResponse() StoredResponse {
	return StoredResponse{Status: 201, Headers: http.Header{"Location": {"/1"}}, Body: []byte("body")}
}
