package app

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

type profileRepo struct {
	user domain.User
	err  error
	uid  int64
	lang string
}

func (r *profileRepo) UpdateLocale(_ context.Context, id int64, locale string) (domain.User, error) {
	r.uid, r.lang = id, locale
	return r.user, r.err
}

func TestUpdateProfileLocale(t *testing.T) {
	repo := &profileRepo{user: domain.User{ID: 5, Locale: "vi"}}
	got, err := (UpdateProfile{Repository: repo}).UpdateLocale(context.Background(), 5, "vi")
	if err != nil || got.Locale != "vi" || repo.uid != 5 || repo.lang != "vi" {
		t.Fatalf("UpdateLocale() = %#v, %v; repo = %#v", got, err, repo)
	}
}

func TestUpdateProfileLocaleRejectsInvalidInputAndRepositoryFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   int64
		lang string
		repo ProfileRepository
		want error
	}{
		{name: "zero user", id: 0, lang: "en", repo: &profileRepo{}, want: ErrInvalidLocale},
		{name: "unsupported language", id: 1, lang: "fr", repo: &profileRepo{}, want: ErrInvalidLocale},
		{name: "missing repository", id: 1, lang: "en", want: errors.New("identity: profile repository is required")},
		{name: "database error", id: 1, lang: "en", repo: &profileRepo{err: errors.New("database")}, want: errors.New("database")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (UpdateProfile{Repository: tc.repo}).UpdateLocale(context.Background(), tc.id, tc.lang)
			if err == nil || err.Error() != tc.want.Error() {
				t.Fatalf("UpdateLocale() error = %v, want %v", err, tc.want)
			}
		})
	}
}
