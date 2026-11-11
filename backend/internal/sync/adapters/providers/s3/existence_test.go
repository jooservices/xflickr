package s3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/jooservices/xflickr/internal/sync/app"
)

type fakeStatClient struct {
	info   minio.ObjectInfo
	err    error
	calls  int
	bucket string
	key    string
}

func (f *fakeStatClient) StatObject(_ context.Context, bucket, key string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	f.calls++
	f.bucket = bucket
	f.key = key
	return f.info, f.err
}

func TestNewObjectCheckerRejectsNilClient(t *testing.T) {
	checker, err := NewObjectChecker(nil)
	if err == nil || checker != nil {
		t.Fatalf("NewObjectChecker(nil) = %v, %v; want nil checker and error", checker, err)
	}
}

func TestExistsWithETagValidationAndCancellation(t *testing.T) {
	client := &fakeStatClient{}
	checker := &ObjectChecker{client: client}

	for _, test := range []struct {
		name   string
		bucket string
		key    string
		etag   string
	}{
		{name: "empty bucket", bucket: "", key: "key", etag: "tag"},
		{name: "blank bucket", bucket: " \t", key: "key", etag: "tag"},
		{name: "empty key", bucket: "bucket", key: "", etag: "tag"},
		{name: "blank key", bucket: "bucket", key: " \t", etag: "tag"},
		{name: "empty etag", bucket: "bucket", key: "key", etag: ""},
		{name: "blank etag", bucket: "bucket", key: "key", etag: " \t"},
		{name: "quoted empty etag", bucket: "bucket", key: "key", etag: `""`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := checker.ExistsWithETag(t.Context(), test.bucket, test.key, test.etag); err == nil {
				t.Fatal("expected invalid input error")
			}
		})
	}
	if _, err := checker.ExistsWithETag(nil, "bucket", "key", "tag"); err == nil {
		t.Fatal("expected nil context error")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := checker.ExistsWithETag(ctx, "bucket", "key", "tag"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v; want context.Canceled", err)
	}
	if client.calls != 0 {
		t.Fatalf("StatObject called %d times for invalid input", client.calls)
	}
}

func TestExistsWithETagMatchesAndDoesNotMatch(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
		got  string
		ok   bool
	}{
		{name: "both unquoted", want: "etag-value", got: "etag-value", ok: true},
		{name: "input quoted", want: `"etag-value"`, got: "etag-value", ok: true},
		{name: "response quoted", want: "etag-value", got: `"etag-value"`, ok: true},
		{name: "both quoted", want: `"etag-value"`, got: `"etag-value"`, ok: true},
		{name: "case differs", want: "ETag-value", got: "etag-value", ok: false},
		{name: "content differs", want: "first", got: "second", ok: false},
		{name: "normalized input empty", want: `""`, got: "", ok: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeStatClient{info: minio.ObjectInfo{ETag: test.got}}
			checker := &ObjectChecker{client: client}
			got, err := checker.ExistsWithETag(t.Context(), " bucket ", " key ", test.want)
			if test.name == "normalized input empty" {
				if err == nil || got {
					t.Fatalf("ExistsWithETag() = %t, %v; want validation error", got, err)
				}
				if client.calls != 0 {
					t.Fatalf("StatObject called %d times for empty normalized ETag", client.calls)
				}
				return
			}
			if err != nil || got != test.ok {
				t.Fatalf("ExistsWithETag() = %t, %v; want %t, nil", got, err, test.ok)
			}
			if client.bucket != " bucket " || client.key != " key " || client.calls != 1 {
				t.Fatalf("StatObject arguments = (%q, %q), calls %d", client.bucket, client.key, client.calls)
			}
		})
	}
}

func TestExistsWithETagMapsProviderErrors(t *testing.T) {
	responseBody := "private-provider-response-body"
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: minio.ErrorResponse{StatusCode: http.StatusNotFound}},
		{name: "unauthorized", err: minio.ErrorResponse{StatusCode: http.StatusUnauthorized}, want: app.ErrAuthentication},
		{name: "forbidden", err: minio.ErrorResponse{StatusCode: http.StatusForbidden}, want: app.ErrAuthentication},
		{name: "too many requests", err: minio.ErrorResponse{StatusCode: http.StatusTooManyRequests}, want: app.ErrRateLimited},
		{name: "slow down", err: minio.ErrorResponse{StatusCode: http.StatusServiceUnavailable, Code: "SlowDown"}, want: app.ErrRateLimited},
		{name: "other provider error", err: minio.ErrorResponse{StatusCode: http.StatusInternalServerError, Message: responseBody}},
		{name: "deadline", err: context.DeadlineExceeded, want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeStatClient{info: minio.ObjectInfo{ETag: "etag"}, err: test.err}
			checker := &ObjectChecker{client: client}
			found, err := checker.ExistsWithETag(t.Context(), "bucket", "key", "etag")
			if test.name == "not found" {
				if found || err != nil {
					t.Fatalf("ExistsWithETag() = %t, %v; want false, nil", found, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected provider error")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("ExistsWithETag() error = %v; want errors.Is(_, %v)", err, test.want)
			}
			if test.name == "other provider error" && strings.Contains(err.Error(), responseBody) {
				t.Fatalf("provider response was exposed in error: %v", err)
			}
		})
	}
}

func TestExistsWithETagUsesS3Head(t *testing.T) {
	var requests atomic.Int32
	var responseCode atomic.Value
	responseCode.Store("ok")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodHead {
			t.Errorf("request method = %q; want HEAD", r.Method)
		}
		if r.URL.EscapedPath() != "/my-bucket/folder/a%20photo.jpg" {
			t.Errorf("request path = %q; want /my-bucket/folder/a%%20photo.jpg", r.URL.EscapedPath())
		}
		switch responseCode.Load().(string) {
		case "ok":
			w.Header().Set("ETag", `"remote-etag"`)
			w.Header().Set("Last-Modified", time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
		case "missing":
			w.WriteHeader(http.StatusNotFound)
		case "unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
		case "forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "throttled":
			w.Header().Set("x-minio-error-code", "SlowDown")
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)

	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds:        credentials.NewStaticV4("fake-access", "fake-secret", ""),
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		MaxRetries:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	checker, err := NewObjectChecker(client)
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("NewObjectChecker made %d requests; want no network request", got)
	}

	for _, test := range []struct {
		name string
		code string
		want bool
		err  error
	}{
		{name: "matching etag", code: "ok", want: true},
		{name: "missing object", code: "missing"},
		{name: "authentication", code: "unauthorized", err: app.ErrAuthentication},
		{name: "forbidden", code: "forbidden", err: app.ErrAuthentication},
		{name: "throttled", code: "throttled", err: app.ErrRateLimited},
	} {
		t.Run(test.name, func(t *testing.T) {
			responseCode.Store(test.code)
			before := requests.Load()
			found, err := checker.ExistsWithETag(t.Context(), "my-bucket", "folder/a photo.jpg", `"remote-etag"`)
			if found != test.want || (test.err == nil && err != nil) || (test.err != nil && !errors.Is(err, test.err)) {
				t.Fatalf("ExistsWithETag() = %t, %v; want %t, %v", found, err, test.want, test.err)
			}
			if got := requests.Load() - before; got != 1 {
				t.Fatalf("request count = %d; want 1", got)
			}
		})
	}
}

func TestNormalizeETagRemovesOneOuterPair(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: `"Tag"`, want: "Tag"},
		{input: `"Tag""`, want: `Tag"`},
		{input: "Tag", want: "Tag"},
		{input: `"Tag`, want: `"Tag`},
	} {
		t.Run(fmt.Sprintf("%q", test.input), func(t *testing.T) {
			if got := normalizeETag(test.input); got != test.want {
				t.Fatalf("normalizeETag(%q) = %q; want %q", test.input, got, test.want)
			}
		})
	}
}
