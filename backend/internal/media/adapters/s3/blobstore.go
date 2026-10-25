// Package s3 implements the S3-compatible BlobStore adapter used with Garage.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"github.com/jooservices/xflickr/internal/media/app"
)

const lifecycleSafetyMargin = 24 * time.Hour

// Config contains credentials and retention settings for an S3-compatible store.
type Config struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	MaxBytes     int64
	MaxTTL       time.Duration
}

// BlobStore stores temporary blobs in one S3 bucket.
type BlobStore struct {
	client   blobClient
	bucket   string
	maxBytes int64
}

type blobClient interface {
	BucketExists(context.Context, string) (bool, error)
	MakeBucket(context.Context, string, minio.MakeBucketOptions) error
	SetBucketLifecycle(context.Context, string, *lifecycle.Configuration) error
	GetBucketLifecycle(context.Context, string) (*lifecycle.Configuration, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
	PresignedGetObject(context.Context, string, string, time.Duration, url.Values) (*url.URL, error)
	ListObjects(context.Context, string, minio.ListObjectsOptions) <-chan minio.ObjectInfo
}

// New creates the S3 client, ensures the configured bucket exists, and installs
// an expiration rule with a one-day safety margin beyond the maximum asset TTL.
func New(ctx context.Context, cfg Config) (*BlobStore, error) {
	if ctx == nil || strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Bucket) == "" ||
		strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || cfg.MaxBytes < 0 || cfg.MaxTTL <= 0 {
		return nil, errors.New("media: invalid s3 blob store configuration")
	}
	endpoint, secure, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: region,
		BucketLookup: func() minio.BucketLookupType {
			if cfg.UsePathStyle {
				return minio.BucketLookupPath
			}
			return minio.BucketLookupAuto
		}(),
	})
	if err != nil {
		return nil, fmt.Errorf("media: create s3 client: %w", err)
	}
	return initialize(ctx, client, cfg.Bucket, cfg.MaxBytes, cfg.MaxTTL, region)
}

func initialize(ctx context.Context, client blobClient, bucket string, maxBytes int64, maxTTL time.Duration, region string) (*BlobStore, error) {
	store := &BlobStore{client: client, bucket: bucket, maxBytes: maxBytes}
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("media: check s3 bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("media: create s3 bucket: %w", err)
		}
	}
	existingLifecycle, err := client.GetBucketLifecycle(ctx, bucket)
	if err != nil && !isMissingLifecycle(err) {
		return nil, fmt.Errorf("media: read s3 lifecycle: %w", err)
	}
	if err := client.SetBucketLifecycle(ctx, bucket, mergeRetentionLifecycle(existingLifecycle, maxTTL)); err != nil {
		return nil, fmt.Errorf("media: configure s3 lifecycle: %w", err)
	}
	return store, nil
}

func parseEndpoint(raw string) (string, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false, errors.New("media: invalid s3 endpoint")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}

func retentionLifecycle(maxTTL time.Duration) *lifecycle.Configuration {
	days := int(math.Ceil(float64(maxTTL+lifecycleSafetyMargin) / float64(24*time.Hour)))
	return &lifecycle.Configuration{Rules: []lifecycle.Rule{{
		ID:         "xflickr-temporary-blobs-expiry",
		Status:     "Enabled",
		Expiration: lifecycle.Expiration{Days: lifecycle.ExpirationDays(days)},
	}}}
}

func mergeRetentionLifecycle(existing *lifecycle.Configuration, maxTTL time.Duration) *lifecycle.Configuration {
	merged := &lifecycle.Configuration{}
	if existing != nil {
		*merged = *existing
		merged.Rules = append([]lifecycle.Rule(nil), existing.Rules...)
	}
	retention := retentionLifecycle(maxTTL).Rules[0]
	for i, rule := range merged.Rules {
		if rule.ID == retention.ID {
			merged.Rules[i] = retention
			return merged
		}
	}
	merged.Rules = append(merged.Rules, retention)
	return merged
}

func isMissingLifecycle(err error) bool {
	var response minio.ErrorResponse
	return errors.As(err, &response) && response.Code == "NoSuchLifecycleConfiguration"
}

func (s *BlobStore) validate(ctx context.Context, key string) error {
	if s == nil || s.client == nil || strings.TrimSpace(s.bucket) == "" || ctx == nil || strings.TrimSpace(key) == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") || strings.Contains(key, "\\") {
		return app.ErrInvalidKey
	}
	return ctx.Err()
}

// Put stores an object with unknown content length using MinIO's streaming upload.
func (s *BlobStore) Put(ctx context.Context, key string, src io.Reader) error {
	if src == nil {
		return errors.New("media: invalid blob put")
	}
	if err := s.validate(ctx, key); err != nil {
		return err
	}
	full, err := s.Full(ctx)
	if err != nil {
		return err
	}
	if full {
		return app.ErrCapacityExceeded
	}
	if _, err := s.client.PutObject(ctx, s.bucket, key, src, -1, minio.PutObjectOptions{}); err != nil {
		return fmt.Errorf("media: put s3 object: %w", err)
	}
	return nil
}

// Get opens an existing object as a stream.
func (s *BlobStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := s.validate(ctx, key); err != nil {
		return nil, err
	}
	if _, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err != nil {
		if isMissing(err) {
			return nil, app.ErrNotFound
		}
		return nil, fmt.Errorf("media: stat s3 object before get: %w", err)
	}
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("media: get s3 object: %w", err)
	}
	return object, nil
}

// Delete removes an object; S3 deletion is idempotent for absent keys.
func (s *BlobStore) Delete(ctx context.Context, key string) error {
	if err := s.validate(ctx, key); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("media: delete s3 object: %w", err)
	}
	return nil
}

// PresignGet returns a time-limited URL for downloading an object.
func (s *BlobStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", errors.New("media: invalid presign request")
	}
	if err := s.validate(ctx, key); err != nil {
		return "", err
	}
	parsed, err := s.client.PresignedGetObject(ctx, s.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("media: presign s3 object: %w", err)
	}
	return parsed.String(), nil
}

// Stat returns object size and modification time.
func (s *BlobStore) Stat(ctx context.Context, key string) (app.BlobInfo, error) {
	if err := s.validate(ctx, key); err != nil {
		return app.BlobInfo{}, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isMissing(err) {
			return app.BlobInfo{}, app.ErrNotFound
		}
		return app.BlobInfo{}, fmt.Errorf("media: stat s3 object: %w", err)
	}
	return app.BlobInfo{Key: key, Size: info.Size, LastModified: info.LastModified}, nil
}

// Usage sums the sizes of all objects in the configured bucket.
func (s *BlobStore) Usage(ctx context.Context) (int64, error) {
	if s == nil || s.client == nil || ctx == nil {
		return 0, errors.New("media: invalid usage request")
	}
	var total int64
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if object.Err != nil {
			return 0, fmt.Errorf("media: list s3 objects: %w", object.Err)
		}
		total += object.Size
	}
	return total, nil
}

// Full reports whether current object usage has reached the configured cap.
func (s *BlobStore) Full(ctx context.Context) (bool, error) {
	if s == nil || s.maxBytes <= 0 {
		return false, nil
	}
	usage, err := s.Usage(ctx)
	if err != nil {
		return false, err
	}
	return usage >= s.maxBytes, nil
}

func isMissing(err error) bool {
	var response minio.ErrorResponse
	if errors.As(err, &response) {
		return response.Code == "NoSuchKey" || response.Code == "NoSuchObject" || response.Code == "NotFound"
	}
	return false
}
