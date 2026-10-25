package local

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/media/app"
)

func TestBlobStorePutGetStatUsagePresignDelete(t *testing.T) {
	store, err := New(t.TempDir(), 32)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Put(ctx, "downloads/photo.bin", strings.NewReader("photo-data")); err != nil {
		t.Fatal(err)
	}
	reader, err := store.Get(ctx, "downloads/photo.bin")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(body) != "photo-data" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	info, err := store.Stat(ctx, "downloads/photo.bin")
	if err != nil || info.Key != "downloads/photo.bin" || info.Size != int64(len("photo-data")) || info.LastModified.IsZero() {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	usage, err := store.Usage(ctx)
	if err != nil || usage != info.Size {
		t.Fatalf("usage=%d err=%v", usage, err)
	}
	full, err := store.Full(ctx)
	if err != nil || full {
		t.Fatalf("full=%v err=%v", full, err)
	}
	url, err := store.PresignGet(ctx, info.Key, time.Minute)
	if err != nil || !strings.HasPrefix(url, "file:") {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if err := store.Delete(ctx, info.Key); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, info.Key); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}
	if _, err := store.Get(ctx, info.Key); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("Get() error=%v", err)
	}
}

func TestBlobStoreCapacityAndOverwrite(t *testing.T) {
	store, err := New(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Put(ctx, "item", strings.NewReader("1234")); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "item", strings.NewReader("12345")); err != nil {
		t.Fatalf("overwrite within cap: %v", err)
	}
	if err := store.Put(ctx, "other", strings.NewReader("x")); !errors.Is(err, app.ErrCapacityExceeded) {
		t.Fatalf("capacity error = %v", err)
	}
	full, err := store.Full(ctx)
	if err != nil || !full {
		t.Fatalf("full=%v err=%v", full, err)
	}
}

func TestBlobStoreRejectsInvalidInputsAndKeys(t *testing.T) {
	if _, err := New("", 1); err == nil {
		t.Fatal("expected empty root error")
	}
	if _, err := New(t.TempDir(), -1); err == nil {
		t.Fatal("expected negative cap error")
	}
	store, err := New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "../escape", "a/../../escape", "/absolute"} {
		if err := store.Put(context.Background(), key, strings.NewReader("x")); !errors.Is(err, app.ErrInvalidKey) {
			t.Errorf("Put(%q) error = %v", key, err)
		}
	}
	if err := store.Put(context.Background(), "ok", nil); err == nil {
		t.Fatal("expected nil reader error")
	}
	if _, err := store.Get(context.Background(), "missing"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("missing get error=%v", err)
	}
	if _, err := store.Stat(context.Background(), "missing"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("missing stat error=%v", err)
	}
	if _, err := store.PresignGet(context.Background(), "missing", time.Minute); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("missing presign error=%v", err)
	}
	if _, err := store.PresignGet(context.Background(), "ok", 0); err == nil {
		t.Fatal("expected invalid ttl error")
	}
	if _, err := store.Get(nil, "ok"); err == nil {
		t.Fatal("expected nil context error")
	}
	if err := store.Delete(nil, "ok"); err == nil {
		t.Fatal("expected nil context error")
	}
	if _, err := store.Stat(nil, "ok"); err == nil {
		t.Fatal("expected nil context error")
	}
	if _, err := store.Usage(nil); err == nil {
		t.Fatal("expected nil context error")
	}
}

func TestBlobStoreHonorsCanceledContexts(t *testing.T) {
	store, err := New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Put(ctx, "item", strings.NewReader("x")); err == nil {
		t.Fatal("Put ignored cancellation")
	}
	if _, err := store.Get(ctx, "item"); err == nil {
		t.Fatal("Get ignored cancellation")
	}
	if err := store.Delete(ctx, "item"); err == nil {
		t.Fatal("Delete ignored cancellation")
	}
	if _, err := store.Stat(ctx, "item"); err == nil {
		t.Fatal("Stat ignored cancellation")
	}
	if _, err := store.Usage(ctx); err == nil {
		t.Fatal("Usage ignored cancellation")
	}
}

func TestBlobStoreUsageSkipsSymlinksAndFullWithoutCap(t *testing.T) {
	root := t.TempDir()
	store, err := New(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain"), []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "plain"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	usage, err := store.Usage(context.Background())
	if err != nil || usage != 3 {
		t.Fatalf("usage=%d err=%v", usage, err)
	}
	full, err := store.Full(context.Background())
	if err != nil || full {
		t.Fatalf("full=%v err=%v", full, err)
	}
}

func TestFilesystemPressureThreshold(t *testing.T) {
	for _, test := range []struct {
		available uint64
		full      bool
	}{
		{available: 11, full: false},
		{available: 10, full: false},
		{available: 9, full: true},
	} {
		if got := filesystemUsageAbove90Percent(100, test.available); got != test.full {
			t.Errorf("available blocks %d: full=%v, want %v", test.available, got, test.full)
		}
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestBlobStorePutAndDeleteSurfaceFilesystemErrors(t *testing.T) {
	root := t.TempDir()
	store, err := New(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "bad", brokenReader{}); err == nil {
		t.Fatal("expected source reader error")
	}
	if err := os.WriteFile(filepath.Join(root, "parent"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "parent/child", strings.NewReader("x")); err == nil {
		t.Fatal("expected parent mkdir error")
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "directory", "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), "directory"); err == nil {
		t.Fatal("expected nonempty directory remove error")
	}
}
