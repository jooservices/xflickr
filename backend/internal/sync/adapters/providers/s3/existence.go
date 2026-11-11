// Package s3 contains the S3-compatible sync adapter components.
package s3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/jooservices/xflickr/internal/sync/app"
)

var (
	errInvalidObjectCheck = errors.New("sync s3: invalid object check")
	errObjectAbsent       = errors.New("sync s3: object absent")
)

type statClient interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

// ObjectChecker checks whether an S3 object has the expected ETag.
type ObjectChecker struct {
	client statClient
}

// NewObjectChecker creates a checker around an already configured MinIO client.
func NewObjectChecker(client *minio.Client) (*ObjectChecker, error) {
	if client == nil {
		return nil, errors.New("sync s3: minio client is required")
	}
	return &ObjectChecker{client: client}, nil
}

// ExistsWithETag compares an object's S3 ETag through MinIO's StatObject operation.
// The configured client may make extra HTTP requests for region discovery or retries.
func (c *ObjectChecker) ExistsWithETag(ctx context.Context, bucket, key, etag string) (bool, error) {
	if ctx == nil {
		return false, errors.New("sync s3: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("sync s3: check object: %w", err)
	}
	if c == nil || c.client == nil || strings.TrimSpace(bucket) == "" || strings.TrimSpace(key) == "" {
		return false, errInvalidObjectCheck
	}
	expectedETag := normalizeETag(etag)
	if strings.TrimSpace(expectedETag) == "" {
		return false, errInvalidObjectCheck
	}

	info, err := c.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		classified := classifyStatError(err)
		if errors.Is(classified, errObjectAbsent) {
			return false, nil
		}
		return false, classified
	}
	return normalizeETag(info.ETag) == expectedETag, nil
}

func normalizeETag(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	return value
}

func classifyStatError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("sync s3: stat object: %w", err)
	}

	response := minio.ToErrorResponse(err)
	switch {
	case response.StatusCode == http.StatusNotFound && response.Code == "NoSuchKey":
		// Only the object HEAD itself reporting a missing key means "absent". Any other 404
		// (for example NoSuchBucket from the region lookup that runs before the HEAD) means
		// the object was never checked and must not be reported as missing.
		return errObjectAbsent
	case response.StatusCode == http.StatusNotFound:
		// The provider controls response.Code, so it is never copied into the error text.
		return errors.New("sync s3: stat object: not found without a missing-key code")
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return fmt.Errorf("sync s3: stat object: %w", app.ErrAuthentication)
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable:
		return fmt.Errorf("sync s3: stat object: %w", app.ErrRateLimited)
	default:
		return errors.New("sync s3: stat object failed")
	}
}
