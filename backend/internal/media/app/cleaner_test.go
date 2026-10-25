package app

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

type fakeAssets struct {
	items     []domain.Asset
	selectErr error
	deleteErr map[int64]error
	deleted   []int64
	limit     int
	onSelect  func()
}

func (f *fakeAssets) ExpiredAssets(_ context.Context, limit int) ([]domain.Asset, error) {
	f.limit = limit
	if f.onSelect != nil {
		f.onSelect()
	}
	return f.items, f.selectErr
}

func (f *fakeAssets) DeleteAsset(_ context.Context, id int64) error {
	if err := f.deleteErr[id]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeBlobs struct {
	err  error
	keys []string
}

func (f *fakeBlobs) Put(context.Context, string, io.Reader) error {
	return nil
}

func (f *fakeBlobs) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}

func (f *fakeBlobs) Delete(_ context.Context, key string) error {
	f.keys = append(f.keys, key)
	return f.err
}

func (f *fakeBlobs) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (f *fakeBlobs) Stat(context.Context, string) (BlobInfo, error) { return BlobInfo{}, nil }
func (f *fakeBlobs) Usage(context.Context) (int64, error)           { return 0, nil }
func (f *fakeBlobs) Full(context.Context) (bool, error)             { return false, nil }

func TestCleanerRunOnceSelectsDeletesBlobThenRow(t *testing.T) {
	assets := &fakeAssets{items: []domain.Asset{{ID: 12, BlobKey: "a"}, {ID: 13, BlobKey: "b"}}}
	blobs := &fakeBlobs{}
	cleaner := Cleaner{Assets: assets, Blobs: blobs}
	if err := cleaner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if assets.limit != DefaultBatchSize || len(blobs.keys) != 2 || len(assets.deleted) != 2 {
		t.Fatalf("limit=%d blobs=%v deleted=%v", assets.limit, blobs.keys, assets.deleted)
	}
}

func TestCleanerRunOnceRetainsRowsAfterErrors(t *testing.T) {
	selectionErr := errors.New("select")
	if err := (Cleaner{Assets: &fakeAssets{selectErr: selectionErr}, Blobs: &fakeBlobs{}}).RunOnce(context.Background()); !errors.Is(err, selectionErr) {
		t.Fatalf("selection error = %v", err)
	}

	rowErr := errors.New("row")
	assets := &fakeAssets{items: []domain.Asset{{ID: 1, BlobKey: "bad-blob"}, {ID: 2, BlobKey: "good-blob"}}, deleteErr: map[int64]error{2: rowErr}}
	blobs := &fakeBlobs{}
	err := (Cleaner{Assets: assets, Blobs: blobs, Batch: -1}).RunOnce(context.Background())
	if !errors.Is(err, rowErr) || len(assets.deleted) != 1 || assets.deleted[0] != 1 {
		t.Fatalf("err=%v deleted=%v", err, assets.deleted)
	}
	if len(blobs.keys) != 2 {
		t.Fatalf("blob deletes = %v", blobs.keys)
	}

	blobErr := errors.New("blob")
	assets = &fakeAssets{items: []domain.Asset{{ID: 3, BlobKey: "bad"}}}
	err = (Cleaner{Assets: assets, Blobs: &fakeBlobs{err: blobErr}}).RunOnce(context.Background())
	if !errors.Is(err, blobErr) || len(assets.deleted) != 0 {
		t.Fatalf("err=%v deleted=%v", err, assets.deleted)
	}
}

func TestCleanerRunOnceRejectsMissingDependencies(t *testing.T) {
	if err := (Cleaner{}).RunOnce(context.Background()); err == nil {
		t.Fatal("expected validation error")
	}
	if err := (Cleaner{Assets: &fakeAssets{}, Blobs: &fakeBlobs{}}).RunOnce(nil); err == nil {
		t.Fatal("expected nil context error")
	}
}

func TestCleanerRunStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (Cleaner{Assets: &fakeAssets{}, Blobs: &fakeBlobs{}, Interval: time.Millisecond}).Run(ctx)
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if err := (Cleaner{}).Run(nil); err == nil {
		t.Fatal("expected nil context error")
	}
	if err := (Cleaner{Assets: &fakeAssets{selectErr: errors.New("select")}, Blobs: &fakeBlobs{}}).Run(context.Background()); err == nil {
		t.Fatal("expected initial cleanup error")
	}

	ctx, cancel = context.WithCancel(context.Background())
	assets := &fakeAssets{}
	assets.onSelect = cancel
	if err := (Cleaner{Assets: assets, Blobs: &fakeBlobs{}, Interval: time.Millisecond}).Run(ctx); err != nil {
		t.Fatalf("Run() after cancellation: %v", err)
	}
}
