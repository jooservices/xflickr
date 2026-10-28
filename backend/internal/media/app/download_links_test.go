package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

type downloadPartsRepoFake struct {
	parts      []DownloadPart
	listErr    error
	fetched    []int
	fetchErr   error
	requeued   int
	requeueErr error
}

func (f *downloadPartsRepoFake) PartsForDownload(context.Context, int64, int64) ([]DownloadPart, error) {
	return f.parts, f.listErr
}

func (f *downloadPartsRepoFake) MarkPartFetched(_ context.Context, _, _ int64, part int) error {
	f.fetched = append(f.fetched, part)
	return f.fetchErr
}

func (f *downloadPartsRepoFake) RetryFailedDownload(context.Context, int64, int64) (int, error) {
	return f.requeued, f.requeueErr
}

type downloadLinksBlobFake struct {
	urls      []string
	keys      []string
	ttls      []time.Duration
	presign   map[string]error
	deleteErr map[string]error
	deleted   []string
}

func (*downloadLinksBlobFake) Put(context.Context, string, io.Reader) error { return nil }
func (*downloadLinksBlobFake) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, ErrNotFound
}

func (f *downloadLinksBlobFake) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return f.deleteErr[key]
}

func (f *downloadLinksBlobFake) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	f.keys = append(f.keys, key)
	f.ttls = append(f.ttls, ttl)
	if err := f.presign[key]; err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://storage.invalid/%s", key)
	f.urls = append(f.urls, url)
	return url, nil
}

func (*downloadLinksBlobFake) Stat(context.Context, string) (BlobInfo, error) {
	return BlobInfo{}, ErrNotFound
}
func (*downloadLinksBlobFake) Usage(context.Context) (int64, error) { return 0, nil }
func (*downloadLinksBlobFake) Full(context.Context) (bool, error)   { return false, nil }

func TestDownloadPartLinksGetCapsLinkTTLAndSignsEveryPart(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	repository := &downloadPartsRepoFake{parts: []DownloadPart{
		{DownloadID: 9, Number: 1, BlobKey: "downloads/9/part-001.zip", Bytes: 100, ExpiresAt: now.Add(time.Hour)},
		{DownloadID: 9, Number: 2, BlobKey: "downloads/9/part-002.zip", Bytes: 200, ExpiresAt: now.Add(5 * time.Minute)},
	}}
	blobs := &downloadLinksBlobFake{}
	links, err := (DownloadPartLinks{Repository: repository, Blobs: blobs, TTL: 15 * time.Minute, Now: func() time.Time { return now }}).Get(context.Background(), 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 || links[0].Number != 1 || links[0].URL != blobs.urls[0] || links[0].Bytes != 100 || !links[0].ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("first link = %+v", links)
	}
	if links[1].Number != 2 || !links[1].ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("second link = %+v", links[1])
	}
	if len(blobs.keys) != 2 || blobs.ttls[0] != 15*time.Minute || blobs.ttls[1] != 5*time.Minute {
		t.Fatalf("presign keys=%v ttls=%v", blobs.keys, blobs.ttls)
	}
}

func TestDownloadPartLinksGetValidationAndFailures(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	validPart := DownloadPart{Number: 1, BlobKey: "part", Bytes: 1, ExpiresAt: now.Add(time.Hour)}
	repository := &downloadPartsRepoFake{parts: []DownloadPart{validPart}}
	blobs := &downloadLinksBlobFake{}
	service := DownloadPartLinks{Repository: repository, Blobs: blobs, Now: func() time.Time { return now }}
	for _, input := range []struct {
		ctx      context.Context
		repo     DownloadPartRepository
		blobs    BlobStore
		userID   int64
		download int64
	}{
		{ctx: nil, repo: repository, blobs: blobs, userID: 1, download: 2},
		{ctx: context.Background(), repo: nil, blobs: blobs, userID: 1, download: 2},
		{ctx: context.Background(), repo: repository, blobs: nil, userID: 1, download: 2},
		{ctx: context.Background(), repo: repository, blobs: blobs, userID: 0, download: 2},
		{ctx: context.Background(), repo: repository, blobs: blobs, userID: 1, download: 0},
	} {
		if _, err := (DownloadPartLinks{Repository: input.repo, Blobs: input.blobs}).Get(input.ctx, input.userID, input.download); err == nil {
			t.Errorf("Get(%v,%d,%d) accepted invalid dependencies", input.ctx, input.userID, input.download)
		}
	}

	repository.listErr = errors.New("read")
	if _, err := service.Get(context.Background(), 1, 2); !errors.Is(err, repository.listErr) {
		t.Fatalf("repository error = %v", err)
	}
	repository.listErr = nil
	repository.parts = nil
	if _, err := service.Get(context.Background(), 1, 2); !errors.Is(err, ErrDownloadNotFound) {
		t.Fatalf("empty parts error = %v", err)
	}

	repository.parts = []DownloadPart{{Number: 1, BlobKey: "part", ExpiresAt: now}}
	if _, err := service.Get(context.Background(), 1, 2); !errors.Is(err, ErrDownloadNotFound) {
		t.Fatalf("expired part error = %v", err)
	}
	repository.parts = []DownloadPart{{Number: 0, BlobKey: "part", ExpiresAt: now.Add(time.Hour)}}
	if _, err := service.Get(context.Background(), 1, 2); err == nil {
		t.Fatal("accepted invalid part metadata")
	}

	repository.parts = []DownloadPart{validPart}
	blobs.presign = map[string]error{"part": ErrNotFound}
	if _, err := service.Get(context.Background(), 1, 2); !errors.Is(err, ErrDownloadNotFound) {
		t.Fatalf("missing blob race error = %v", err)
	}
	blobs.presign = map[string]error{"part": errors.New("sign")}
	if _, err := service.Get(context.Background(), 1, 2); err == nil {
		t.Fatal("presign error was ignored")
	}

	blobs.presign = nil
	service.TTL = 0
	links, err := service.Get(context.Background(), 1, 2)
	if err != nil || len(links) != 1 || blobs.ttls[len(blobs.ttls)-1] != DefaultDownloadLinkTTL {
		t.Fatalf("default TTL link = %+v err=%v ttls=%v", links, err, blobs.ttls)
	}
}

func TestDownloadPartLinksMarkFetchedAndRetryValidateAndDelegate(t *testing.T) {
	repository := &downloadPartsRepoFake{requeued: 2}
	links := DownloadPartLinks{Repository: repository}
	for _, input := range []struct {
		ctx context.Context
		r   DownloadPartRepository
		u   int64
		d   int64
		p   int
	}{
		{ctx: nil, r: repository, u: 1, d: 2, p: 1},
		{ctx: context.Background(), r: nil, u: 1, d: 2, p: 1},
		{ctx: context.Background(), r: repository, u: 0, d: 2, p: 1},
		{ctx: context.Background(), r: repository, u: 1, d: 0, p: 1},
		{ctx: context.Background(), r: repository, u: 1, d: 2, p: 0},
	} {
		if err := (DownloadPartLinks{Repository: input.r}).MarkFetched(input.ctx, input.u, input.d, input.p); err == nil {
			t.Fatal("MarkFetched accepted invalid input")
		}
	}
	repository.fetchErr = errors.New("ack")
	if err := links.MarkFetched(context.Background(), 1, 2, 3); !errors.Is(err, repository.fetchErr) {
		t.Fatalf("MarkFetched error = %v", err)
	}
	repository.fetchErr = nil
	if err := links.MarkFetched(context.Background(), 1, 2, 3); err != nil || len(repository.fetched) != 2 {
		t.Fatalf("MarkFetched err=%v fetched=%v", err, repository.fetched)
	}

	retry := RetryFailed{Repository: repository}
	for _, input := range []struct {
		ctx context.Context
		r   FailedDownloadRepository
		u   int64
		d   int64
	}{
		{ctx: nil, r: repository, u: 1, d: 2},
		{ctx: context.Background(), r: nil, u: 1, d: 2},
		{ctx: context.Background(), r: repository, u: 0, d: 2},
		{ctx: context.Background(), r: repository, u: 1, d: 0},
	} {
		if _, err := (RetryFailed{Repository: input.r}).Run(input.ctx, input.u, input.d); err == nil {
			t.Fatal("RetryFailed accepted invalid input")
		}
	}
	repository.requeueErr = errors.New("queue")
	if _, err := retry.Run(context.Background(), 1, 2); !errors.Is(err, repository.requeueErr) {
		t.Fatalf("RetryFailed error = %v", err)
	}
	repository.requeueErr = nil
	if got, err := retry.Run(context.Background(), 1, 2); err != nil || got != 2 {
		t.Fatalf("RetryFailed = %d, %v", got, err)
	}
}

type downloadExpiryRepoFake struct {
	parts          []ExpiredDownloadPart
	selectErr      error
	deleteErr      map[int64]error
	markErr        map[int64]error
	downloadMarked []int64
	partDeleted    []ExpiredDownloadPart
	partlessLimit  int
	partlessErr    error
}

func (f *downloadExpiryRepoFake) ExpiredDownloadParts(_ context.Context, limit int) ([]ExpiredDownloadPart, error) {
	f.partlessLimit = limit
	return f.parts, f.selectErr
}

func (f *downloadExpiryRepoFake) DeleteDownloadPart(_ context.Context, downloadID int64, partNumber int) error {
	part := ExpiredDownloadPart{DownloadID: downloadID, Number: partNumber}
	if err := f.deleteErr[downloadID]; err != nil {
		return err
	}
	f.partDeleted = append(f.partDeleted, part)
	return nil
}

func (f *downloadExpiryRepoFake) MarkDownloadExpired(_ context.Context, id int64) error {
	f.downloadMarked = append(f.downloadMarked, id)
	return f.markErr[id]
}

func (f *downloadExpiryRepoFake) MarkExpiredDownloads(_ context.Context, limit int) error {
	f.partlessLimit = limit
	return f.partlessErr
}

func TestDownloadExpiryCleanerRunOnceDeletesThenRemovesMetadata(t *testing.T) {
	repository := &downloadExpiryRepoFake{parts: []ExpiredDownloadPart{
		{DownloadID: 10, Number: 1, BlobKey: "part-1"},
		{DownloadID: 10, Number: 2, BlobKey: "part-2"},
	}}
	blobs := &downloadLinksBlobFake{deleteErr: map[string]error{"part-2": ErrNotFound}}
	cleaner := DownloadExpiryCleaner{Repository: repository, Blobs: blobs}
	if err := cleaner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repository.partlessLimit != DefaultBatchSize || len(blobs.deleted) != 2 || len(repository.partDeleted) != 2 || len(repository.downloadMarked) != 1 || repository.downloadMarked[0] != 10 {
		t.Fatalf("limit=%d blobs=%v parts=%v marked=%v", repository.partlessLimit, blobs.deleted, repository.partDeleted, repository.downloadMarked)
	}
}

func TestDownloadExpiryCleanerRetainsRowsAndReportsErrors(t *testing.T) {
	selectErr := errors.New("select")
	if err := (DownloadExpiryCleaner{Repository: &downloadExpiryRepoFake{selectErr: selectErr}, Blobs: &downloadLinksBlobFake{}}).RunOnce(context.Background()); !errors.Is(err, selectErr) {
		t.Fatalf("selection error = %v", err)
	}
	deleteErr := errors.New("delete")
	repository := &downloadExpiryRepoFake{parts: []ExpiredDownloadPart{{DownloadID: 1, Number: 1, BlobKey: "bad"}}, deleteErr: map[int64]error{1: deleteErr}}
	blobs := &downloadLinksBlobFake{deleteErr: map[string]error{"bad": deleteErr}}
	err := (DownloadExpiryCleaner{Repository: repository, Blobs: blobs, Batch: 1}).RunOnce(context.Background())
	if !errors.Is(err, deleteErr) || len(repository.partDeleted) != 0 || len(repository.downloadMarked) != 1 {
		t.Fatalf("delete failure err=%v deleted=%v marked=%v", err, repository.partDeleted, repository.downloadMarked)
	}

	rowErr := errors.New("metadata")
	repository = &downloadExpiryRepoFake{parts: []ExpiredDownloadPart{{DownloadID: 2, Number: 1, BlobKey: "part"}}, deleteErr: map[int64]error{2: rowErr}}
	blobs = &downloadLinksBlobFake{}
	repository.markErr = map[int64]error{2: rowErr}
	repository.partlessErr = rowErr
	err = (DownloadExpiryCleaner{Repository: repository, Blobs: blobs}).RunOnce(context.Background())
	if !errors.Is(err, rowErr) || len(repository.partDeleted) != 0 {
		t.Fatalf("metadata failure err=%v deleted=%v", err, repository.partDeleted)
	}
}

func TestDownloadExpiryCleanerRejectsInvalidInputAndRunsCanceled(t *testing.T) {
	if err := (DownloadExpiryCleaner{}).RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce accepted missing dependencies")
	}
	if err := (DownloadExpiryCleaner{Repository: &downloadExpiryRepoFake{}, Blobs: &downloadLinksBlobFake{}}).RunOnce(nil); err == nil {
		t.Fatal("RunOnce accepted nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cleaner := DownloadExpiryCleaner{Repository: &downloadExpiryRepoFake{selectErr: errors.New("select")}, Blobs: &downloadLinksBlobFake{}, Logger: logger}
	if err := cleaner.Run(ctx); err != nil {
		t.Fatalf("Run canceled context = %v", err)
	}
	if err := cleaner.Run(nil); err == nil {
		t.Fatal("Run accepted nil context")
	}
}
