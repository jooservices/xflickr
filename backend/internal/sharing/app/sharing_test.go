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

type readerStub struct {
	setting domain.Setting
	shared  int64
	err     error
}

func (r readerStub) Get(context.Context, int64) (domain.Setting, error) {
	return r.setting, r.err
}

func (r readerStub) CountShared(context.Context, int64) (int64, error) {
	return r.shared, r.err
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

func TestReadExecute(t *testing.T) {
	if _, err := (Read{}).Execute(context.Background(), 0); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("invalid user error = %v", err)
	}
	if _, err := (Read{}).Execute(context.Background(), 3); err == nil {
		t.Fatal("expected missing reader error")
	}
	want := domain.Summary{Setting: domain.Setting{UserID: 3, Enabled: true}, SharedRecordCount: 4}
	got, err := (Read{Repository: readerStub{setting: want.Setting, shared: 4}}).Execute(context.Background(), 3)
	if err != nil || got != want {
		t.Fatalf("summary = %+v, err = %v", got, err)
	}
	storageErr := errors.New("storage")
	if _, err := (Read{Repository: readerStub{err: storageErr}}).Execute(context.Background(), 3); !errors.Is(err, storageErr) {
		t.Fatalf("read error = %v", err)
	}
}
