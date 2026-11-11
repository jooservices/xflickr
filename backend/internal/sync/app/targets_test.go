package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/crypto"
)

const targetTestToken = "opaque-target-token"

func TestTargetsCreateSealsTokenWithTargetAAD(t *testing.T) {
	repository := &targetRepositoryFake{id: 42}
	sealer := &targetSealerFake{sealed: targetTestSealed()}
	service := Targets{Repository: repository, Sealer: sealer}
	config := json.RawMessage(`{"folder":"archive"}`)
	got, err := service.Create(t.Context(), Target{
		UserID:   7,
		Provider: " drive ",
		Label:    " Archive ",
		Config:   config,
	}, []byte(targetTestToken))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.UserID != 7 || got.Provider != "drive" || got.Label != "Archive" || got.Status != "connected" {
		t.Fatalf("created target = %+v", got)
	}
	if string(got.Config) != string(config) {
		t.Fatalf("config = %s, want %s", got.Config, config)
	}
	if repository.createCalls != 1 || repository.created.ID != 42 || repository.created.Status != "connected" {
		t.Fatalf("repository create calls = %d, target = %+v", repository.createCalls, repository.created)
	}
	if !bytes.Equal(sealer.plaintext, []byte(targetTestToken)) || string(sealer.aad) != "sync_targets:42:token" {
		t.Fatalf("sealer received matching token: %t, aad = %q", bytes.Equal(sealer.plaintext, []byte(targetTestToken)), sealer.aad)
	}
	if string(TargetTokenAAD(42)) != "sync_targets:42:token" {
		t.Fatalf("target token aad = %q", TargetTokenAAD(42))
	}
}

func TestTargetsCreateRejectsInvalidInput(t *testing.T) {
	validTarget := Target{UserID: 7, Provider: "drive", Label: "Archive", Config: json.RawMessage(`{}`)}
	cases := []struct {
		name     string
		service  Targets
		ctx      context.Context
		target   Target
		token    []byte
		wantErr  error
		wantText string
	}{
		{name: "missing repository", service: Targets{Sealer: &targetSealerFake{}}, ctx: t.Context(), target: validTarget, token: []byte(targetTestToken), wantText: "incomplete"},
		{name: "missing sealer", service: Targets{Repository: &targetRepositoryFake{}}, ctx: t.Context(), target: validTarget, token: []byte(targetTestToken), wantText: "sealer"},
		{name: "nil context", service: targetTestService(), target: validTarget, token: []byte(targetTestToken), wantText: "context"},
		{name: "canceled context", service: targetTestService(), ctx: canceledTargetContext(t), target: validTarget, token: []byte(targetTestToken), wantErr: context.Canceled},
		{name: "nonpositive user", service: targetTestService(), ctx: t.Context(), target: Target{Provider: "drive", Label: "Archive", Config: json.RawMessage(`{}`)}, token: []byte(targetTestToken), wantErr: ErrInvalidTarget},
		{name: "blank provider", service: targetTestService(), ctx: t.Context(), target: Target{UserID: 7, Provider: " \t", Label: "Archive", Config: json.RawMessage(`{}`)}, token: []byte(targetTestToken), wantErr: ErrInvalidTarget},
		{name: "blank label", service: targetTestService(), ctx: t.Context(), target: Target{UserID: 7, Provider: "drive", Label: " \n", Config: json.RawMessage(`{}`)}, token: []byte(targetTestToken), wantErr: ErrInvalidTarget},
		{name: "empty token", service: targetTestService(), ctx: t.Context(), target: validTarget, wantErr: ErrInvalidTarget},
		{name: "malformed config", service: targetTestService(), ctx: t.Context(), target: Target{UserID: 7, Provider: "drive", Label: "Archive", Config: json.RawMessage(`{bad`)}, token: []byte(targetTestToken), wantErr: ErrInvalidTarget},
		{name: "array config", service: targetTestService(), ctx: t.Context(), target: Target{UserID: 7, Provider: "drive", Label: "Archive", Config: json.RawMessage(`[]`)}, token: []byte(targetTestToken), wantErr: ErrInvalidTarget},
		{name: "null config", service: targetTestService(), ctx: t.Context(), target: Target{UserID: 7, Provider: "drive", Label: "Archive", Config: json.RawMessage(`null`)}, token: []byte(targetTestToken), wantErr: ErrInvalidTarget},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.service.Create(test.ctx, test.target, test.token)
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
			if test.wantText != "" && (err == nil || !strings.Contains(err.Error(), test.wantText)) {
				t.Fatalf("error = %v, want text %q", err, test.wantText)
			}
			if repository, ok := test.service.Repository.(*targetRepositoryFake); ok && repository.createCalls != 0 {
				t.Fatalf("invalid target reached repository %d times", repository.createCalls)
			}
		})
	}
}

func TestTargetsPropagateFailures(t *testing.T) {
	insertErr := errors.New("insert failed")
	repository := &targetRepositoryFake{id: 12, createErr: insertErr}
	service := Targets{Repository: repository, Sealer: &targetSealerFake{sealed: targetTestSealed()}}
	_, err := service.Create(t.Context(), Target{UserID: 7, Provider: "drive", Label: "Archive", Config: json.RawMessage(`{}`)}, []byte(targetTestToken))
	if !errors.Is(err, insertErr) || !strings.Contains(err.Error(), "create target") {
		t.Fatalf("create error = %v", err)
	}

	sealErr := errors.New("seal failed while handling " + targetTestToken)
	repository = &targetRepositoryFake{id: 13}
	service = Targets{Repository: repository, Sealer: &targetSealerFake{err: sealErr}}
	_, err = service.Create(t.Context(), Target{UserID: 7, Provider: "drive", Label: "Archive", Config: json.RawMessage(`{}`)}, []byte(targetTestToken))
	if err == nil || !errors.Is(err, sealErr) || !strings.Contains(err.Error(), "seal target token") || strings.Contains(err.Error(), targetTestToken) {
		t.Fatal("seal failure must retain its cause and context without exposing the token")
	}
	if repository.inserted {
		t.Fatal("repository inserted a target after sealing failed")
	}

	readErr := errors.New("read failed")
	repository = &targetRepositoryFake{getErr: readErr}
	service = Targets{Repository: repository, Sealer: &targetSealerFake{}}
	if _, err := service.Get(t.Context(), 7, 12); !errors.Is(err, readErr) || !strings.Contains(err.Error(), "get target") {
		t.Fatalf("get error = %v", err)
	}
}

func TestTargetsGetEnforcesOwnership(t *testing.T) {
	want := Target{ID: 42, UserID: 7, Provider: "drive", Label: "Archive", Status: "connected", Config: json.RawMessage(`{"folder":"archive"}`)}
	repository := &targetRepositoryFake{target: want}
	service := Targets{Repository: repository}
	got, err := service.Get(t.Context(), 7, 42)
	if err != nil || got.ID != want.ID || got.UserID != want.UserID || string(got.Config) != string(want.Config) {
		t.Fatalf("get = %+v, error = %v", got, err)
	}
	if repository.getUserID != 7 || repository.getID != 42 {
		t.Fatalf("get owner = %d, id = %d", repository.getUserID, repository.getID)
	}

	for _, test := range []struct {
		name string
		user int64
		id   int64
	}{
		{name: "other owner", user: 8, id: 42},
		{name: "missing id", user: 7, id: 99},
	} {
		t.Run(test.name, func(t *testing.T) {
			missing := &targetRepositoryFake{getErr: ErrTargetNotFound}
			service := Targets{Repository: missing}
			_, err := service.Get(t.Context(), test.user, test.id)
			if !errors.Is(err, ErrTargetNotFound) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func targetTestService() Targets {
	return Targets{Repository: &targetRepositoryFake{id: 1}, Sealer: &targetSealerFake{sealed: targetTestSealed()}}
}

func canceledTargetContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

func targetTestSealed() crypto.Sealed {
	return crypto.Sealed{KeyID: "test", Nonce: []byte{1}, Ciphertext: []byte{2}, WrappedDEK: []byte{3}}
}

type targetSealerFake struct {
	plaintext []byte
	aad       []byte
	sealed    crypto.Sealed
	err       error
}

func (sealer *targetSealerFake) Seal(plaintext, aad []byte) (crypto.Sealed, error) {
	sealer.plaintext = append([]byte(nil), plaintext...)
	sealer.aad = append([]byte(nil), aad...)
	if sealer.err != nil {
		return crypto.Sealed{}, sealer.err
	}
	return sealer.sealed, nil
}

type targetRepositoryFake struct {
	id          int64
	target      Target
	created     Target
	createCalls int
	getUserID   int64
	getID       int64
	getErr      error
	createErr   error
	inserted    bool
}

func (repository *targetRepositoryFake) Create(_ context.Context, target Target, seal func(int64) (crypto.Sealed, error)) (Target, error) {
	repository.createCalls++
	if repository.id == 0 {
		repository.id = 1
	}
	if _, err := seal(repository.id); err != nil {
		return Target{}, err
	}
	if err := repository.createErr; err != nil {
		return Target{}, err
	}
	target.ID = repository.id
	repository.created = target
	repository.inserted = true
	return target, nil
}

func (repository *targetRepositoryFake) Get(_ context.Context, userID, id int64) (Target, error) {
	repository.getUserID = userID
	repository.getID = id
	if repository.getErr != nil {
		return Target{}, repository.getErr
	}
	return repository.target, nil
}
