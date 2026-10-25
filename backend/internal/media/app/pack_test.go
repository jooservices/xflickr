package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type packBlobStore struct {
	objects   map[string][]byte
	putErr    error
	deleteErr string
	deleteKey string
}

func (s *packBlobStore) Put(ctx context.Context, key string, src io.Reader) error {
	if s.putErr != nil {
		return s.putErr
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	s.objects[key] = data
	return nil
}

func (s *packBlobStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *packBlobStore) Delete(_ context.Context, key string) error {
	if key == s.deleteKey && s.deleteErr != "" {
		err := errors.New(s.deleteErr)
		s.deleteErr = ""
		return err
	}
	delete(s.objects, key)
	return nil
}

func (s *packBlobStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (s *packBlobStore) Stat(_ context.Context, key string) (BlobInfo, error) {
	data, ok := s.objects[key]
	if !ok {
		return BlobInfo{}, ErrNotFound
	}
	return BlobInfo{Key: key, Size: int64(len(data))}, nil
}
func (s *packBlobStore) Usage(context.Context) (int64, error) { return 0, nil }
func (s *packBlobStore) Full(context.Context) (bool, error)   { return false, nil }

type packRepo struct {
	items  []PackItem
	ttl    time.Duration
	status string
	parts  []ZIPPart
	err    error
}

func (r *packRepo) PackItems(context.Context, int64) ([]PackItem, time.Duration, error) {
	return r.items, r.ttl, nil
}

func (r *packRepo) PackedOriginals(context.Context, int64) ([]string, bool, error) {
	if r.status != "" || len(r.parts) > 0 {
		var keys []string
		for _, item := range r.items {
			if item.Failure == "" {
				keys = append(keys, item.BlobKey)
			}
		}
		return keys, true, nil
	}
	return nil, false, nil
}

func (r *packRepo) CompletePack(_ context.Context, _ int64, status string, parts []ZIPPart) error {
	if r.err != nil {
		return r.err
	}
	r.status, r.parts = status, parts
	return nil
}

func TestParsePackJob(t *testing.T) {
	job, err := ParsePackJob([]byte(`{"download_id":42}`))
	if err != nil || job.DownloadID != 42 {
		t.Fatalf("ParsePackJob() = %+v, %v", job, err)
	}
	for _, payload := range []string{"{", `{ "download_id": 0 }`, `{ "download_id": "42" }`} {
		if _, err := ParsePackJob([]byte(payload)); err == nil {
			t.Errorf("ParsePackJob(%q) accepted invalid payload", payload)
		}
	}
}

func TestPackerWritesTwentyItemsIntoThreePartsAndCleansOriginals(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	blobs := &packBlobStore{objects: map[string][]byte{}}
	repo := &packRepo{ttl: 2 * time.Hour}
	for id := int64(1); id <= 20; id++ {
		key := fmt.Sprintf("originals/%d", id)
		data := bytes.Repeat([]byte{byte(id)}, 100)
		blobs.objects[key] = data
		repo.items = append(repo.items, PackItem{PhotoID: id, Name: fmt.Sprintf("photo-%02d.jpg", id), BlobKey: key, Size: int64(len(data))})
	}
	repo.items = append(repo.items, PackItem{PhotoID: 21, Name: "failed.jpg", Failure: "download failed"})
	packer := Packer{Blobs: blobs, Repo: repo, Now: func() time.Time { return now }, Limit: 3000}
	if err := packer.Pack(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if len(repo.parts) != 3 {
		t.Fatalf("parts = %d, want 3", len(repo.parts))
	}
	if repo.status != "ready_with_errors" {
		t.Fatalf("status = %q", repo.status)
	}
	for _, part := range repo.parts {
		if part.ExpiresAt != now.Add(2*time.Hour) {
			t.Errorf("expiry = %v", part.ExpiresAt)
		}
		if part.Bytes > 3000 {
			t.Errorf("part %d size = %d", part.Number, part.Bytes)
		}
		reader, err := blobs.Get(ctx, part.BlobKey)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range archive.File {
			if file.Method != zip.Store {
				t.Errorf("%s method = %d", file.Name, file.Method)
			}
			if file.Name == "manifest.csv" {
				contents, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				manifest, err := io.ReadAll(contents)
				_ = contents.Close()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(manifest), "download failed") || !strings.Contains(string(manifest), "photo-20.jpg") {
					t.Errorf("incomplete manifest: %s", manifest)
				}
			}
		}
	}
	for id := int64(1); id <= 20; id++ {
		if _, ok := blobs.objects[fmt.Sprintf("originals/%d", id)]; ok {
			t.Errorf("original %d was not deleted", id)
		}
	}
}

func TestPackSplitAndValidationErrors(t *testing.T) {
	if err := (Packer{}).Pack(context.Background(), 1); err == nil {
		t.Fatal("accepted missing dependencies")
	}
	if err := (Packer{Blobs: &packBlobStore{}, Repo: &packRepo{}}).Pack(context.Background(), 0); err == nil {
		t.Fatal("accepted invalid id")
	}
	if err := (Packer{Blobs: &packBlobStore{}, Repo: &packRepo{}}).Pack(context.Background(), 1); err == nil {
		t.Fatal("accepted no items")
	}
	if _, err := splitPackItems([]PackItem{{PhotoID: 1, Name: "oversize", Size: 100}}, 1); err == nil {
		t.Fatal("accepted oversize item")
	}
	if _, err := splitPackItems(nil, 0); err == nil {
		t.Fatal("accepted zero limit")
	}
	if err := (Packer{Blobs: &packBlobStore{}, Repo: &packRepo{items: []PackItem{{PhotoID: 1, Size: -1}}}}).Pack(context.Background(), 1); err == nil {
		t.Fatal("accepted invalid source metadata")
	}
}

func TestPackerPropagatesStorageAndRepositoryErrors(t *testing.T) {
	items := []PackItem{{PhotoID: 1, Name: "one", BlobKey: "original", Size: 3}}
	t.Run("read", func(t *testing.T) {
		err := (Packer{Blobs: &packBlobStore{}, Repo: &packRepo{items: items}}).Pack(context.Background(), 1)
		if err == nil {
			t.Fatal("accepted missing source")
		}
	})
	t.Run("put", func(t *testing.T) {
		blobs := &packBlobStore{objects: map[string][]byte{"original": []byte("abc")}, putErr: errors.New("write failed")}
		err := (Packer{Blobs: blobs, Repo: &packRepo{items: items}, Limit: 1000}).Pack(context.Background(), 1)
		if err == nil {
			t.Fatal("ignored put failure")
		}
	})
	t.Run("complete", func(t *testing.T) {
		blobs := &packBlobStore{objects: map[string][]byte{"original": []byte("abc")}}
		err := (Packer{Blobs: blobs, Repo: &packRepo{items: items, err: errors.New("db failed")}, Limit: 1000}).Pack(context.Background(), 1)
		if err == nil {
			t.Fatal("ignored completion failure")
		}
		if _, ok := blobs.objects["original"]; !ok {
			t.Fatal("deleted original before persistence")
		}
	})
}

func TestPackerRetryAfterPartialOriginalCleanup(t *testing.T) {
	blobs := &packBlobStore{objects: map[string][]byte{"original": []byte("abc")}, deleteKey: "original", deleteErr: "temporary delete failure"}
	repo := &packRepo{items: []PackItem{{PhotoID: 1, Name: "one", BlobKey: "original", Size: 3}}}
	packer := Packer{Blobs: blobs, Repo: repo, Limit: 1000}
	if err := packer.Pack(context.Background(), 9); err == nil {
		t.Fatal("expected first cleanup attempt to fail")
	}
	if repo.status != "ready" || len(repo.parts) != 1 {
		t.Fatalf("completion was not committed before cleanup: status=%q parts=%d", repo.status, len(repo.parts))
	}
	if err := packer.Pack(context.Background(), 9); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if _, ok := blobs.objects["original"]; ok {
		t.Fatal("retry did not remove remaining original")
	}
	if len(repo.parts) != 1 {
		t.Fatalf("retry repacked existing output: parts=%d", len(repo.parts))
	}
}

func TestPackerCleansEarlierPartsAfterLaterPartFailure(t *testing.T) {
	items := []PackItem{{PhotoID: 1, Name: "one", BlobKey: "one", Size: 1}, {PhotoID: 2, Name: "two", BlobKey: "two", Size: 1}}
	blobs := &packBlobStore{objects: map[string][]byte{"one": []byte("1"), "two": []byte("2")}}
	repo := &packRepo{items: items}
	// The first part is emitted, then Stat of the second part fails.
	packer := Packer{Blobs: &statFailPackBlobStore{packBlobStore: blobs, failKey: "downloads/4/part-002.zip"}, Repo: repo, Limit: 200}
	if err := packer.Pack(context.Background(), 4); err == nil {
		t.Fatal("expected later part inspection to fail")
	}
	for key := range blobs.objects {
		if strings.HasPrefix(key, "downloads/") {
			t.Errorf("orphan ZIP part remains: %q", key)
		}
	}
}

type statFailPackBlobStore struct {
	*packBlobStore
	failKey string
}

func (s *statFailPackBlobStore) Stat(ctx context.Context, key string) (BlobInfo, error) {
	if key == s.failKey {
		return BlobInfo{}, errors.New("stat failed")
	}
	return s.packBlobStore.Stat(ctx, key)
}
