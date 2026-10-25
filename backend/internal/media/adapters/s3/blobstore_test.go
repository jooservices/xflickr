package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"github.com/jooservices/xflickr/internal/media/app"
)

type fakeClient struct {
	exists          bool
	checkErr        error
	makeErr         error
	lifecycleErr    error
	lifecycleGetErr error
	putErr          error
	statErr         error
	getErr          error
	deleteErr       error
	presignErr      error
	listErr         error
	objects         []minio.ObjectInfo
	stat            minio.ObjectInfo
	lifecycle       *lifecycle.Configuration
	putCalled       bool
	getCalled       bool
	deleted         bool
	presignTTL      time.Duration
}

func (f *fakeClient) BucketExists(context.Context, string) (bool, error) {
	return f.exists, f.checkErr
}

func (f *fakeClient) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return f.makeErr
}

func (f *fakeClient) SetBucketLifecycle(_ context.Context, _ string, config *lifecycle.Configuration) error {
	f.lifecycle = config
	return f.lifecycleErr
}

func (f *fakeClient) GetBucketLifecycle(context.Context, string) (*lifecycle.Configuration, error) {
	return f.lifecycle, f.lifecycleGetErr
}

func (f *fakeClient) PutObject(_ context.Context, _ string, _ string, reader io.Reader, _ int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	f.putCalled = true
	contents, err := io.ReadAll(reader)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	f.objects = append(f.objects, minio.ObjectInfo{Size: int64(len(contents))})
	return minio.UploadInfo{}, f.putErr
}

func (f *fakeClient) StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return f.stat, f.statErr
}

func (f *fakeClient) GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
	f.getCalled = true
	return nil, f.getErr
}

func (f *fakeClient) RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error {
	f.deleted = true
	return f.deleteErr
}

func (f *fakeClient) PresignedGetObject(_ context.Context, _, _ string, ttl time.Duration, _ url.Values) (*url.URL, error) {
	f.presignTTL = ttl
	if f.presignErr != nil {
		return nil, f.presignErr
	}
	return url.Parse("https://objects.invalid/bucket/key?signature=test")
}

func (f *fakeClient) ListObjects(_ context.Context, _ string, _ minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	capacity := len(f.objects)
	if f.listErr != nil {
		capacity++
	}
	ch := make(chan minio.ObjectInfo, capacity)
	for _, object := range f.objects {
		ch <- object
	}
	if f.listErr != nil {
		ch <- minio.ObjectInfo{Err: f.listErr}
	}
	close(ch)
	return ch
}

func TestInitializeCreatesBucketAndInstallsSafetyLifecycle(t *testing.T) {
	client := &fakeClient{}
	store, err := initialize(context.Background(), client, "assets", 100, 25*time.Hour, "us-east-1")
	if err != nil || store == nil {
		t.Fatalf("initialize: store=%v err=%v", store, err)
	}
	if client.lifecycle == nil || len(client.lifecycle.Rules) != 1 || client.lifecycle.Rules[0].Expiration.Days != 3 {
		t.Fatalf("unexpected lifecycle: %#v", client.lifecycle)
	}
	client = &fakeClient{exists: true}
	if _, err := initialize(context.Background(), client, "assets", 0, time.Hour, "us-east-1"); err != nil {
		t.Fatal(err)
	}
	if client.lifecycle.Rules[0].Expiration.Days != 2 {
		t.Fatalf("want 2 day expiry, got %d", client.lifecycle.Rules[0].Expiration.Days)
	}
}

func TestInitializePreservesOtherLifecycleRules(t *testing.T) {
	other := lifecycle.Rule{ID: "operator-rule", Status: "Enabled", Expiration: lifecycle.Expiration{Days: 30}}
	managed := lifecycle.Rule{ID: "xflickr-temporary-blobs-expiry", Status: "Enabled", Expiration: lifecycle.Expiration{Days: 9}}
	client := &fakeClient{exists: true, lifecycle: &lifecycle.Configuration{Rules: []lifecycle.Rule{other, managed}}}
	if _, err := initialize(context.Background(), client, "assets", 0, 25*time.Hour, "us-east-1"); err != nil {
		t.Fatal(err)
	}
	if len(client.lifecycle.Rules) != 2 || client.lifecycle.Rules[0].ID != other.ID || client.lifecycle.Rules[0].Expiration.Days != 30 {
		t.Fatalf("operator lifecycle rule was changed: %#v", client.lifecycle.Rules)
	}
	if client.lifecycle.Rules[1].ID != managed.ID || client.lifecycle.Rules[1].Expiration.Days != 3 {
		t.Fatalf("managed lifecycle rule was not updated: %#v", client.lifecycle.Rules)
	}
}

func TestInitializeErrors(t *testing.T) {
	for _, client := range []*fakeClient{{checkErr: errors.New("check")}, {makeErr: errors.New("make")}, {lifecycleGetErr: errors.New("lifecycle read")}, {lifecycleErr: errors.New("lifecycle write")}} {
		if _, err := initialize(context.Background(), client, "assets", 0, time.Hour, "us-east-1"); err == nil {
			t.Fatal("expected initialization error")
		}
	}
}

func TestNewValidatesAndParsesConfiguration(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("expected invalid configuration")
	}
	if _, _, err := parseEndpoint("ftp://objects.invalid"); err == nil {
		t.Fatal("expected invalid scheme")
	}
	if _, _, err := parseEndpoint("https://objects.invalid/basepath"); err == nil {
		t.Fatal("expected invalid endpoint path")
	}
	if host, secure, err := parseEndpoint("https://objects.invalid:9000"); err != nil || host != "objects.invalid:9000" || !secure {
		t.Fatalf("parse https: %q %t %v", host, secure, err)
	}
	if host, secure, err := parseEndpoint("http://localhost:9000"); err != nil || host != "localhost:9000" || secure {
		t.Fatalf("parse http: %q %t %v", host, secure, err)
	}
}

func TestNewInitializesS3ClientAndLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("lifecycle") {
			w.Header().Set("Content-Type", "application/xml")
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LifecycleConfiguration>`)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	store, err := New(context.Background(), Config{
		Endpoint: server.URL, Bucket: "assets", AccessKey: "access", SecretKey: "secret",
		UsePathStyle: true, MaxTTL: 24 * time.Hour,
	})
	if err != nil || store == nil {
		t.Fatalf("New: store=%v err=%v", store, err)
	}
}

func TestBlobOperations(t *testing.T) {
	client := &fakeClient{stat: minio.ObjectInfo{Size: 7, LastModified: time.Unix(10, 0)}, objects: []minio.ObjectInfo{{Size: 8}}}
	store := &BlobStore{client: client, bucket: "assets", maxBytes: 15}
	if err := store.Put(context.Background(), "key", strings.NewReader("payload")); err != nil || !client.putCalled {
		t.Fatalf("put: %v", err)
	}
	if _, err := store.Get(context.Background(), "key"); err != nil || !client.getCalled {
		t.Fatalf("get: %v", err)
	}
	info, err := store.Stat(context.Background(), "key")
	if err != nil || info.Key != "key" || info.Size != 7 || info.LastModified.Unix() != 10 {
		t.Fatalf("stat: %#v %v", info, err)
	}
	link, err := store.PresignGet(context.Background(), "key", time.Minute)
	if err != nil || !strings.Contains(link, "signature=test") || client.presignTTL != time.Minute {
		t.Fatalf("presign: %q %v", link, err)
	}
	if err := store.Delete(context.Background(), "key"); err != nil || !client.deleted {
		t.Fatalf("delete: %v", err)
	}
	usage, err := store.Usage(context.Background())
	if err != nil || usage != 15 {
		t.Fatalf("usage: %d %v", usage, err)
	}
	full, err := store.Full(context.Background())
	if err != nil || !full {
		t.Fatalf("full: %t %v", full, err)
	}
	store.maxBytes = 0
	full, err = store.Full(context.Background())
	if err != nil || full {
		t.Fatalf("unlimited full: %t %v", full, err)
	}
}

func TestPutRejectsWhenCapacityIsFull(t *testing.T) {
	client := &fakeClient{objects: []minio.ObjectInfo{{Size: 10}}}
	store := &BlobStore{client: client, bucket: "assets", maxBytes: 10}
	if err := store.Put(context.Background(), "key", strings.NewReader("x")); !errors.Is(err, app.ErrCapacityExceeded) {
		t.Fatalf("put with full store: %v", err)
	}
	if client.putCalled {
		t.Fatal("put object called after capacity was reached")
	}
}

func TestBlobOperationsRejectInvalidAndCanceledRequests(t *testing.T) {
	store := &BlobStore{client: &fakeClient{}, bucket: "assets"}
	for _, key := range []string{"", "/absolute", "../escape", "a\\b"} {
		if err := store.Put(context.Background(), key, strings.NewReader("x")); !errors.Is(err, app.ErrInvalidKey) {
			t.Fatalf("key %q: %v", key, err)
		}
	}
	if err := store.Put(context.Background(), "key", nil); err == nil {
		t.Fatal("expected nil reader error")
	}
	if _, err := store.Get(context.Background(), "key"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := store.PresignGet(context.Background(), "key", 0); err == nil {
		t.Fatal("expected invalid TTL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Delete(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delete: %v", err)
	}
	if _, err := store.Usage(nil); err == nil {
		t.Fatal("expected nil context usage error")
	}
	var missing *BlobStore
	if _, err := missing.Usage(context.Background()); err == nil {
		t.Fatal("expected nil store usage error")
	}
}

func TestMissingAndStorageErrors(t *testing.T) {
	missing := minio.ErrorResponse{Code: "NoSuchKey"}
	if !isMissing(missing) || isMissing(errors.New("no")) {
		t.Fatal("missing detection mismatch")
	}
	client := &fakeClient{statErr: missing}
	store := &BlobStore{client: client, bucket: "assets", maxBytes: 10}
	if _, err := store.Stat(context.Background(), "gone"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("stat missing: %v", err)
	}
	if _, err := store.Get(context.Background(), "gone"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	client = &fakeClient{putErr: errors.New("put")}
	store.client = client
	if err := store.Put(context.Background(), "key", strings.NewReader("x")); err == nil {
		t.Fatal("expected put error")
	}
	client = &fakeClient{deleteErr: errors.New("delete")}
	store.client = client
	if err := store.Delete(context.Background(), "key"); err == nil {
		t.Fatal("expected delete error")
	}
	client = &fakeClient{statErr: errors.New("stat")}
	store.client = client
	if _, err := store.Stat(context.Background(), "key"); err == nil {
		t.Fatal("expected stat error")
	}
	if _, err := store.Get(context.Background(), "key"); err == nil {
		t.Fatal("expected get preflight error")
	}
	client = &fakeClient{getErr: errors.New("get")}
	store.client = client
	if _, err := store.Get(context.Background(), "key"); err == nil {
		t.Fatal("expected get error")
	}
	client = &fakeClient{presignErr: errors.New("presign")}
	store.client = client
	if _, err := store.PresignGet(context.Background(), "key", time.Minute); err == nil {
		t.Fatal("expected presign error")
	}
	client = &fakeClient{listErr: errors.New("list")}
	store.client = client
	if _, err := store.Usage(context.Background()); err == nil {
		t.Fatal("expected list error")
	}
	if _, err := store.Full(context.Background()); err == nil {
		t.Fatal("expected full usage error")
	}
}
