package app

import (
	"context"
	"errors"
	"testing"

	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

func TestAddUsesEffectiveDepthAndIgnoresTooDeep(t *testing.T) {
	repository := &interestRepositoryFake{}
	service := Interests{Repository: repository, Settings: effectiveSettingsFake{maxDepth: map[int64]int{1: 2}}}
	if err := service.Add(t.Context(), 1, "person", 2, "contact"); err != nil {
		t.Fatal(err)
	}
	if err := service.Add(t.Context(), 1, "too-deep", 3, "favorite"); err != nil {
		t.Fatal(err)
	}
	if len(repository.added) != 1 || repository.added[0].PersonNSID != "person" {
		t.Fatalf("added interests = %+v", repository.added)
	}
}

func TestExpandableUsesDepthPerUser(t *testing.T) {
	repository := &interestRepositoryFake{interests: []crawldomain.InterestDepth{{UserID: 1, Depth: 1}, {UserID: 2, Depth: 3}}}
	service := Interests{Repository: repository, Settings: effectiveSettingsFake{maxDepth: map[int64]int{1: 2, 2: 3}}}
	got, err := service.Expandable(t.Context(), "person")
	if err != nil || !got {
		t.Fatalf("Expandable() = %v, %v, want true", got, err)
	}
	if len(repository.requested) != 1 || repository.requested[0] != "person" {
		t.Fatalf("requested people = %v", repository.requested)
	}
}

func TestHandleConnectedAddsOwnerAtDepthZero(t *testing.T) {
	repository := &interestRepositoryFake{}
	service := Interests{Repository: repository, Settings: effectiveSettingsFake{maxDepth: map[int64]int{7: 1}}}
	if err := service.HandleConnected(t.Context(), []byte(`{"user_id":7,"nsid":"owner","depth":0}`)); err != nil {
		t.Fatal(err)
	}
	want := crawldomain.Interest{UserID: 7, PersonNSID: "owner", Depth: 0, Via: "connected_account"}
	if len(repository.added) != 1 || repository.added[0] != want {
		t.Fatalf("added interests = %+v, want %+v", repository.added, want)
	}
}

func TestAddRejectsInvalidInputAndDependencies(t *testing.T) {
	settings := effectiveSettingsFake{maxDepth: map[int64]int{1: 2}}
	for _, test := range []struct {
		name    string
		service Interests
		ctx     context.Context
		user    int64
		person  string
		depth   int
		via     string
		want    error
	}{
		{name: "repository", service: Interests{Settings: settings}, ctx: t.Context(), user: 1, person: "person", via: "contact", want: ErrRepositoryRequired},
		{name: "settings", service: Interests{Repository: &interestRepositoryFake{}}, ctx: t.Context(), user: 1, person: "person", via: "contact", want: ErrSettingsRequired},
		{name: "context", service: Interests{Repository: &interestRepositoryFake{}, Settings: settings}, user: 1, person: "person", via: "contact", want: ErrContextRequired},
		{name: "invalid", service: Interests{Repository: &interestRepositoryFake{}, Settings: settings}, ctx: t.Context(), user: 1, person: "person", depth: -1, via: "contact", want: crawldomain.ErrInvalidInterest},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.service.Add(test.ctx, test.user, test.person, test.depth, test.via)
			if !errors.Is(err, test.want) {
				t.Fatalf("Add() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestExpandablePropagatesRepositoryAndSettingsErrors(t *testing.T) {
	want := errors.New("failed")
	repository := &interestRepositoryFake{listErr: want}
	service := Interests{Repository: repository, Settings: effectiveSettingsFake{}}
	if _, err := service.Expandable(t.Context(), "person"); !errors.Is(err, want) {
		t.Fatalf("Expandable() error = %v, want %v", err, want)
	}
	repository.listErr = nil
	repository.interests = []crawldomain.InterestDepth{{UserID: 4, Depth: 0}}
	service.Settings = effectiveSettingsFake{err: want}
	if _, err := service.Expandable(t.Context(), "person"); !errors.Is(err, want) {
		t.Fatalf("Expandable() settings error = %v, want %v", err, want)
	}
}

func TestHandleConnectedRejectsMalformedOrInvalidEvent(t *testing.T) {
	service := Interests{Repository: &interestRepositoryFake{}, Settings: effectiveSettingsFake{maxDepth: map[int64]int{1: 2}}}
	if err := service.HandleConnected(t.Context(), []byte("{")); err == nil {
		t.Fatal("HandleConnected() accepted malformed JSON")
	}
	if err := service.HandleConnected(t.Context(), []byte(`{"user_id":0,"nsid":"owner","depth":0}`)); !errors.Is(err, crawldomain.ErrInvalidInterest) {
		t.Fatalf("HandleConnected() error = %v, want invalid interest", err)
	}
}

type interestRepositoryFake struct {
	added     []crawldomain.Interest
	interests []crawldomain.InterestDepth
	requested []string
	listErr   error
}

func (repository *interestRepositoryFake) Add(_ context.Context, interest crawldomain.Interest) error {
	repository.added = append(repository.added, interest)
	return nil
}

func (repository *interestRepositoryFake) ForPerson(_ context.Context, person string) ([]crawldomain.InterestDepth, error) {
	repository.requested = append(repository.requested, person)
	return repository.interests, repository.listErr
}

type effectiveSettingsFake struct {
	maxDepth map[int64]int
	err      error
}

func (settings effectiveSettingsFake) Effective(_ context.Context, userID int64) (settingsdomain.Effective, error) {
	return settingsdomain.Effective{MaxDepth: settings.maxDepth[userID]}, settings.err
}
