package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/media/app"
)

// TestS3Integration exercises a pre-provisioned Garage or other S3-compatible
// endpoint. Set S3_TEST_* variables to a dedicated test bucket; it never starts
// a service or changes a developer's configured production bucket.
func TestS3Integration(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	bucket := os.Getenv("S3_TEST_BUCKET")
	accessKey := os.Getenv("S3_TEST_ACCESS_KEY")
	secretKey := os.Getenv("S3_TEST_SECRET_KEY")
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		t.Skip("set S3_TEST_ENDPOINT, S3_TEST_BUCKET, S3_TEST_ACCESS_KEY, and S3_TEST_SECRET_KEY to run S3 integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := New(ctx, Config{
		Endpoint: endpoint, Region: os.Getenv("S3_TEST_REGION"), Bucket: bucket,
		AccessKey: accessKey, SecretKey: secretKey, UsePathStyle: true,
		MaxBytes: int64(len("garage blob round trip")), MaxTTL: 25 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "xflickr-integration-" + time.Now().UTC().Format("20060102T150405.000000000")
	defer store.Delete(context.Background(), key)
	want := "garage blob round trip"
	if err := store.Put(ctx, key, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	info, err := store.Stat(ctx, key)
	if err != nil || info.Size != int64(len(want)) {
		t.Fatalf("stat: info=%#v err=%v", info, err)
	}
	reader, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(got) != want {
		t.Fatalf("get result %q, read error %v, close error %v", got, readErr, closeErr)
	}
	presigned, err := store.PresignGet(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	response, err := http.Get(presigned)
	if err != nil {
		t.Fatalf("fetch presigned URL: %v", err)
	}
	got, readErr = io.ReadAll(response.Body)
	closeErr = response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || string(got) != want {
		t.Fatalf("presigned GET status=%d content=%q read error=%v close error=%v", response.StatusCode, got, readErr, closeErr)
	}
	usage, err := store.Usage(ctx)
	if err != nil || usage < int64(len(want)) {
		t.Fatalf("usage: %d, %v", usage, err)
	}
	full, err := store.Full(ctx)
	if err != nil || !full {
		t.Fatalf("full at configured cap: %t, %v", full, err)
	}
	// New installs the max-TTL-plus-one-day safety rule on this bucket.
	lifecycle, err := store.client.GetBucketLifecycle(ctx, bucket)
	if err != nil || lifecycle == nil {
		t.Fatalf("bucket lifecycle not installed: %v", err)
	}
	var expiryFound bool
	for _, rule := range lifecycle.Rules {
		if rule.ID == "xflickr-temporary-blobs-expiry" {
			expiryFound = rule.Status == "Enabled" && rule.Expiration.Days == 3
		}
	}
	if !expiryFound {
		t.Fatalf("max TTL plus safety margin lifecycle rule missing or incorrect: %#v", lifecycle.Rules)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stat(ctx, key); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("deleted object should be absent, got %v", err)
	}
	usage, err = store.Usage(ctx)
	if err != nil || usage != 0 {
		t.Fatalf("usage after delete: %d, %v", usage, err)
	}
	full, err = store.Full(ctx)
	if err != nil || full {
		t.Fatalf("store should be below cap after delete: full=%t err=%v", full, err)
	}
}
