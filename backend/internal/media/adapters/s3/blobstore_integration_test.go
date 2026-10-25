package s3

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
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
		MaxTTL: 24 * time.Hour,
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
	reader, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(got) != want {
		t.Fatalf("get result %q, read error %v, close error %v", got, readErr, closeErr)
	}
	if _, err := store.PresignGet(ctx, key, time.Minute); err != nil {
		t.Fatalf("presign: %v", err)
	}
	// New installs the max-TTL-plus-one-day safety rule on this bucket.
	lifecycle, err := store.client.GetBucketLifecycle(ctx, bucket)
	if err != nil || lifecycle == nil || len(lifecycle.Rules) == 0 {
		t.Fatalf("bucket lifecycle not installed: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stat(ctx, key); err != nil {
		t.Fatalf("deleted object should be absent: %v", err)
	}
}
