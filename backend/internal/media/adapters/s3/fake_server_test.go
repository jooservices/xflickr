package s3

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const fakeBucket = "assets"

// fakeS3Server answers the object requests minio-go sends for BlobStore, from
// memory, with the status codes and XML error bodies S3 returns. Keys are
// path-style (/<bucket>/<key>); unknown-length uploads go through multipart.
type fakeS3Server struct {
	mu      sync.Mutex
	objects map[string][]byte
	uploads map[string]map[int][]byte
	nextID  int
}

// newFakeS3Client starts an empty in-memory S3 endpoint and returns a real
// minio client that talks to it over HTTP.
func newFakeS3Client(t *testing.T) *minio.Client {
	t.Helper()
	server := httptest.NewServer(&fakeS3Server{objects: map[string][]byte{}, uploads: map[string]map[int][]byte{}})
	t.Cleanup(server.Close)
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{
		Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	return client
}

func (s *fakeS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != fakeBucket {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.route(w, r, key)
}

func (s *fakeS3Server) route(w http.ResponseWriter, r *http.Request, key string) {
	query := r.URL.Query()
	switch r.Method {
	case http.MethodPost:
		if query.Has("uploads") {
			s.initiate(w, key)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body) // S3 reads the part list before it answers
		s.complete(w, key, query.Get("uploadId"))
	case http.MethodPut:
		s.part(w, r, query)
	case http.MethodGet, http.MethodHead:
		s.get(w, key)
	case http.MethodDelete:
		delete(s.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "The specified method is not allowed against this resource.")
	}
}

func (s *fakeS3Server) initiate(w http.ResponseWriter, key string) {
	s.nextID++
	id := strconv.Itoa(s.nextID)
	s.uploads[id] = map[int][]byte{}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprintf(w, `<InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, fakeBucket, key, id)
}

func (s *fakeS3Server) part(w http.ResponseWriter, r *http.Request, query url.Values) {
	parts, ok := s.uploads[query.Get("uploadId")]
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.")
		return
	}
	number, err := strconv.Atoi(query.Get("partNumber"))
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "InvalidArgument", "Part number must be an integer.")
		return
	}
	data, err := readPayload(r)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "IncompleteBody", "You did not provide the number of bytes specified by the Content-Length HTTP header.")
		return
	}
	parts[number] = data
	w.Header().Set("ETag", `"part"`)
	w.WriteHeader(http.StatusOK)
}

// complete concatenates parts 1..n in order into the object, as S3 does.
func (s *fakeS3Server) complete(w http.ResponseWriter, key, id string) {
	parts, ok := s.uploads[id]
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.")
		return
	}
	var data []byte
	for n := 1; n <= len(parts); n++ {
		part, ok := parts[n]
		if !ok {
			writeS3Error(w, http.StatusBadRequest, "InvalidPart", "One or more of the specified parts could not be found.")
			return
		}
		data = append(data, part...)
	}
	s.objects[key] = data
	delete(s.uploads, id)
	w.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprintf(w, `<CompleteMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><ETag>"complete"</ETag></CompleteMultipartUploadResult>`, fakeBucket, key)
}

// get serves GET and HEAD; net/http omits the body for HEAD.
func (s *fakeS3Server) get(w http.ResponseWriter, key string) {
	data, ok := s.objects[key]
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, message)
}
