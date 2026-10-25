package app

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/sharing/domain"
)

type repositoryStub struct {
	setting domain.Setting
	err     error
}

func (r *repositoryStub) Set(_ context.Context, setting domain.Setting) (domain.Setting, error) {
	r.setting = setting
	if r.err != nil {
		return domain.Setting{}, r.err
	}
	return setting, nil
}

func TestChangeExecute(t *testing.T) {
	if _, err := (Change{}).Execute(context.Background(), 0, true); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("invalid user error = %v", err)
	}
	if _, err := (Change{}).Execute(context.Background(), 1, true); err == nil {
		t.Fatal("expected missing repository error")
	}
	wantErr := errors.New("storage")
	repo := &repositoryStub{err: wantErr}
	if _, err := (Change{Repository: repo}).Execute(context.Background(), 9, true); !errors.Is(err, wantErr) {
		t.Fatalf("repository error = %v", err)
	}
	repo.err = nil
	got, err := (Change{Repository: repo}).Execute(context.Background(), 9, true)
	if err != nil || !got.Enabled || got.UserID != 9 || repo.setting != got {
		t.Fatalf("setting = %+v, err = %v", got, err)
	}
}
