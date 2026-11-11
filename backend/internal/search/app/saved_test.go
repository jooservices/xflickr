package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/search/app"
)

var errSavedSearchBackend = errors.New("saved search backend failed")

type fakeSavedRepository struct {
	created     app.SavedSearch
	fetched     app.SavedSearch
	createErr   error
	getErr      error
	createCalls int
	getCalls    int
	userID      int64
	query       string
	getUserID   int64
	getID       int64
}

func (repository *fakeSavedRepository) Create(_ context.Context, userID int64, query string) (app.SavedSearch, error) {
	repository.createCalls++
	repository.userID = userID
	repository.query = query
	return repository.created, repository.createErr
}

func (repository *fakeSavedRepository) Get(_ context.Context, userID, id int64) (app.SavedSearch, error) {
	repository.getCalls++
	repository.getUserID = userID
	repository.getID = id
	return repository.fetched, repository.getErr
}

func TestSavedSearchesCreate(t *testing.T) {
	wantTime := time.Date(2026, time.November, 11, 8, 30, 0, 123000, time.UTC)
	wantSearch := app.SavedSearch{ID: 41, UserID: 7, Query: "cats 雪", CreatedAt: wantTime}
	tests := []struct {
		name       string
		userID     int64
		query      string
		repository *fakeSavedRepository
		wantQuery  string
		wantSearch app.SavedSearch
		wantErr    error
		wantCalls  int
	}{
		{
			name:       "trims and preserves unicode",
			userID:     7,
			query:      " \t cats 雪 \n",
			repository: &fakeSavedRepository{created: wantSearch},
			wantQuery:  "cats 雪",
			wantSearch: wantSearch,
			wantCalls:  1,
		},
		{
			name:       "accepts one thousand unicode runes",
			userID:     7,
			query:      strings.Repeat("界", 1000),
			repository: &fakeSavedRepository{created: wantSearch},
			wantQuery:  strings.Repeat("界", 1000),
			wantSearch: wantSearch,
			wantCalls:  1,
		},
		{name: "rejects zero user", userID: 0, query: "cats", repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "rejects negative user", userID: -1, query: "cats", repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "rejects empty query", userID: 7, query: " \t\n", repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "rejects nul query", userID: 7, query: "cat\x00s", repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "rejects more than one thousand runes", userID: 7, query: strings.Repeat("雪", 1001), repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "preserves repository error", userID: 7, query: "cats", repository: &fakeSavedRepository{createErr: errSavedSearchBackend}, wantQuery: "cats", wantErr: errSavedSearchBackend, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := app.SavedSearches{Repository: test.repository}
			got, err := service.Create(t.Context(), test.userID, test.query)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, test.wantErr)
			}
			if test.repository.createCalls != test.wantCalls {
				t.Fatalf("repository Create() calls = %d, want %d", test.repository.createCalls, test.wantCalls)
			}
			if test.wantCalls == 1 {
				if test.repository.userID != test.userID || test.repository.query != test.wantQuery {
					t.Fatalf("repository input = (%d, %q), want (%d, %q)", test.repository.userID, test.repository.query, test.userID, test.wantQuery)
				}
				if err == nil && got != test.wantSearch {
					t.Fatalf("Create() = %+v, want %+v", got, test.wantSearch)
				}
			}
		})
	}

	t.Run("rejects a missing repository", func(t *testing.T) {
		_, err := (app.SavedSearches{}).Create(t.Context(), 7, "cats")
		if err == nil || !strings.Contains(err.Error(), "search:") {
			t.Fatalf("Create() error = %v, want contextual repository error", err)
		}
	})
}

func TestSavedSearchesGet(t *testing.T) {
	want := app.SavedSearch{
		ID: 41, UserID: 7, Query: "cats 雪",
		CreatedAt: time.Date(2026, time.November, 11, 8, 30, 0, 123000, time.UTC),
	}
	tests := []struct {
		name       string
		userID     int64
		id         int64
		repository *fakeSavedRepository
		want       app.SavedSearch
		wantErr    error
		wantCalls  int
	}{
		{name: "reads by user and id", userID: 7, id: 41, repository: &fakeSavedRepository{fetched: want}, want: want, wantCalls: 1},
		{name: "rejects zero user", userID: 0, id: 41, repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "rejects negative user", userID: -1, id: 41, repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "rejects zero id", userID: 7, id: 0, repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "rejects negative id", userID: 7, id: -1, repository: &fakeSavedRepository{}, wantErr: app.ErrInvalidSavedSearch},
		{name: "preserves not found", userID: 8, id: 41, repository: &fakeSavedRepository{getErr: app.ErrNotFound}, wantErr: app.ErrNotFound, wantCalls: 1},
		{name: "preserves backend error", userID: 7, id: 41, repository: &fakeSavedRepository{getErr: errSavedSearchBackend}, wantErr: errSavedSearchBackend, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := app.SavedSearches{Repository: test.repository}
			got, err := service.Get(t.Context(), test.userID, test.id)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, test.wantErr)
			}
			if test.repository.getCalls != test.wantCalls {
				t.Fatalf("repository Get() calls = %d, want %d", test.repository.getCalls, test.wantCalls)
			}
			if test.wantCalls == 1 && (test.repository.getUserID != test.userID || test.repository.getID != test.id) {
				t.Fatalf("repository input = (%d, %d), want (%d, %d)", test.repository.getUserID, test.repository.getID, test.userID, test.id)
			}
			if err == nil && got != test.want {
				t.Fatalf("Get() = %+v, want %+v", got, test.want)
			}
		})
	}

	t.Run("rejects a missing repository", func(t *testing.T) {
		_, err := (app.SavedSearches{}).Get(t.Context(), 7, 41)
		if err == nil || !strings.Contains(err.Error(), "search:") {
			t.Fatalf("Get() error = %v, want contextual repository error", err)
		}
	})
}
