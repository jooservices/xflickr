package app

import (
	"context"
	"errors"
	"testing"
	"time"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
)

type downloadsFake struct {
	candidates []catalogapp.DownloadCandidate
	created    mediadomain.Download
	err        error
	calls      int
}

func (f *downloadsFake) DownloadCandidates(context.Context, string, string, []int64) ([]catalogapp.DownloadCandidate, error) {
	return f.candidates, f.err
}

func (f *downloadsFake) Create(_ context.Context, d mediadomain.Download, _ []DownloadItemCandidate) (mediadomain.Download, error) {
	f.calls++
	f.created = d
	return d, f.err
}

func TestDownloadsCreateAppliesPolicyAndChoosesItemState(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	repo := &downloadsFake{candidates: []catalogapp.DownloadCandidate{
		{PhotoID: 10, OwnerNSID: "owner", Visibility: "public", SizesConfirmed: true, Observers: []catalogapp.DownloadObserver{{UserID: 1}}},
		{PhotoID: 11, OwnerNSID: "owner", Visibility: "friends"},
		{PhotoID: 12, OwnerNSID: "other", Visibility: "private", Observers: []catalogapp.DownloadObserver{{UserID: 2, SharingEnabled: true}}},
	}}
	service := Downloads{Catalog: catalogapp.DownloadCatalog{Photos: repo}, Repository: repo, Now: func() time.Time { return now }, TTL: time.Hour}
	result, err := service.Create(context.Background(), CreateDownloadInput{UserID: 1, Kind: mediadomain.DownloadContact, Subject: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 2 || len(result.Items) != 1 || result.Items[0].PhotoID != 10 || result.Items[0].State != mediadomain.ItemPending {
		t.Fatalf("download=%+v", result)
	}
	if !result.CreatedAt.Equal(now) || !result.ExpiresAt.Equal(now.Add(time.Hour)) || repo.calls != 1 {
		t.Fatalf("download=%+v calls=%d", result, repo.calls)
	}
}

func TestDownloadsCreateWaitingSizesAndRepositoryError(t *testing.T) {
	repo := &downloadsFake{candidates: []catalogapp.DownloadCandidate{{PhotoID: 7, OwnerNSID: "x", Visibility: "private", Observers: []catalogapp.DownloadObserver{{UserID: 1}}}}}
	service := Downloads{Catalog: catalogapp.DownloadCatalog{Photos: repo}, Repository: repo}
	result, err := service.Create(context.Background(), CreateDownloadInput{UserID: 1, Kind: mediadomain.DownloadAlbum, Subject: "a"})
	if err != nil || result.Items[0].State != mediadomain.ItemWaitingSizes {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	repo.err = errors.New("database")
	if _, err = service.Create(context.Background(), CreateDownloadInput{UserID: 1, Kind: mediadomain.DownloadAlbum, Subject: "a"}); err == nil {
		t.Fatal("expected repository error")
	}
}

func TestDownloadsRejectWhenTemporaryStorageIsFull(t *testing.T) {
	repo := &downloadsFake{}
	service := Downloads{
		Catalog:    catalogapp.DownloadCatalog{Photos: repo},
		Repository: repo,
		Pressure:   interface{ Check(context.Context) error }(pressureFailure{}),
	}
	_, err := service.Create(context.Background(), CreateDownloadInput{UserID: 1, Kind: mediadomain.DownloadAlbum, Subject: "album"})
	if !errors.Is(err, ErrTempStorageFull) || repo.calls != 0 {
		t.Fatalf("Create() err=%v repository calls=%d", err, repo.calls)
	}
}

type pressureFailure struct{}

func (pressureFailure) Check(context.Context) error { return ErrTempStorageFull }

func TestDownloadsRejectInvalidSelection(t *testing.T) {
	for _, input := range []CreateDownloadInput{
		{},
		{UserID: 1, Kind: mediadomain.DownloadContact, Subject: ""},
		{UserID: 1, Kind: mediadomain.DownloadAlbum, Subject: "album", PhotoIDs: []int64{3}},
		{UserID: 1, Kind: mediadomain.DownloadTray, PhotoIDs: []int64{3, 3}},
		{UserID: 1, Kind: mediadomain.DownloadTray, PhotoIDs: []int64{0}},
	} {
		if _, err := (Downloads{Repository: &downloadsFake{}}).Create(context.Background(), input); !errors.Is(err, ErrInvalidDownloadRequest) {
			t.Fatalf("input=%+v err=%v", input, err)
		}
	}
	if _, err := (Downloads{}).Create(context.Background(), CreateDownloadInput{UserID: 1, Kind: mediadomain.DownloadTray, PhotoIDs: []int64{1}}); !errors.Is(err, ErrInvalidDownloadRequest) {
		t.Fatalf("missing repo err=%v", err)
	}
}

func TestDownloadsRejectSelectionsOverLimit(t *testing.T) {
	photos := make([]int64, MaxDownloadItems+1)
	for i := range photos {
		photos[i] = int64(i + 1)
	}
	if _, err := (Downloads{Repository: &downloadsFake{}}).Create(context.Background(), CreateDownloadInput{UserID: 1, Kind: mediadomain.DownloadTray, PhotoIDs: photos}); !errors.Is(err, ErrInvalidDownloadRequest) {
		t.Fatalf("oversized tray error = %v", err)
	}
	catalog := catalogapp.DownloadCatalog{Photos: &downloadsFake{candidates: make([]catalogapp.DownloadCandidate, MaxDownloadItems+1)}}
	if _, err := (Downloads{Catalog: catalog, Repository: &downloadsFake{}}).Create(context.Background(), CreateDownloadInput{UserID: 1, Kind: mediadomain.DownloadAlbum, Subject: "large"}); !errors.Is(err, ErrInvalidDownloadRequest) {
		t.Fatalf("oversized album error = %v", err)
	}
}
