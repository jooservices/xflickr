package http

import (
	"context"
	"errors"
	"testing"
	"time"

	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type downloadServiceFake struct {
	result mediadomain.Download
	err    error
	input  mediaapp.CreateDownloadInput
}

func (f *downloadServiceFake) Create(_ context.Context, input mediaapp.CreateDownloadInput) (mediadomain.Download, error) {
	f.input = input
	return f.result, f.err
}

type downloadPartsServiceFake struct {
	links    []mediaapp.DownloadPartLink
	getErr   error
	fetched  []int
	fetchErr error
	userID   int64
	download int64
}

func (f *downloadPartsServiceFake) Get(_ context.Context, userID, downloadID int64) ([]mediaapp.DownloadPartLink, error) {
	f.userID, f.download = userID, downloadID
	return f.links, f.getErr
}

func (f *downloadPartsServiceFake) MarkFetched(_ context.Context, userID, downloadID int64, partNumber int) error {
	f.userID, f.download = userID, downloadID
	f.fetched = append(f.fetched, partNumber)
	return f.fetchErr
}

type retryDownloadServiceFake struct {
	count    int
	err      error
	userID   int64
	download int64
}

func (f *retryDownloadServiceFake) Run(_ context.Context, userID, downloadID int64) (int, error) {
	f.userID, f.download = userID, downloadID
	return f.count, f.err
}

func TestCreateDownloadAuthValidationAndSuccess(t *testing.T) {
	principal := func(context.Context) (int64, bool) { return 9, true }
	if got, _ := (DownloadHandler{}).CreateDownload(context.Background(), gen.CreateDownloadRequestObject{}); got.(gen.CreateDownload401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("unauth response=%#v", got)
	}
	if got, _ := (DownloadHandler{Principal: func(context.Context) (int64, bool) { return 0, false }}).CreateDownload(context.Background(), gen.CreateDownloadRequestObject{}); got.(gen.CreateDownload401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("principal response=%#v", got)
	}
	h := DownloadHandler{Principal: principal, Downloads: &downloadServiceFake{}}
	for _, req := range []gen.CreateDownloadRequestObject{{}, {Body: nil}} {
		got, _ := h.CreateDownload(context.Background(), req)
		if _, ok := got.(gen.CreateDownload400JSONResponse); !ok {
			t.Fatalf("invalid response=%#v", got)
		}
	}
	photos := []int64{2, 3}
	subject := "album"
	fake := &downloadServiceFake{result: mediadomain.Download{ID: 5, Kind: mediadomain.DownloadAlbum, Subject: subject, Status: "pending", CreatedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), Items: []mediadomain.DownloadItem{{PhotoID: 2}, {PhotoID: 3}}, Skipped: 1}}
	h.Downloads = fake
	got, err := h.CreateDownload(context.Background(), gen.CreateDownloadRequestObject{Body: &gen.CreateDownloadRequest{Kind: gen.CreateDownloadRequestKindAlbum, Subject: &subject, PhotoIds: &photos}})
	if err != nil {
		t.Fatal(err)
	}
	response := got.(gen.CreateDownload201JSONResponse)
	if response.ID != 5 || response.ItemCount != 2 || response.Skipped != 1 || fake.input.UserID != 9 || fake.input.Subject != subject || len(fake.input.PhotoIDs) != 2 {
		t.Fatalf("response=%+v input=%+v", response, fake.input)
	}
}

func TestCreateDownloadMapsUseCaseErrors(t *testing.T) {
	h := DownloadHandler{Principal: func(context.Context) (int64, bool) { return 1, true }, Downloads: &downloadServiceFake{err: mediaapp.ErrInvalidDownloadRequest}}
	got, err := h.CreateDownload(context.Background(), gen.CreateDownloadRequestObject{Body: &gen.CreateDownloadRequest{Kind: gen.CreateDownloadRequestKindTray}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(gen.CreateDownload400JSONResponse); !ok {
		t.Fatalf("invalid response=%#v", got)
	}
	h.Downloads = &downloadServiceFake{err: mediadomain.ErrInvalidDownload}
	got, err = h.CreateDownload(context.Background(), gen.CreateDownloadRequestObject{Body: &gen.CreateDownloadRequest{Kind: gen.CreateDownloadRequestKindTray}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(gen.CreateDownload400JSONResponse); !ok {
		t.Fatalf("domain response=%#v", got)
	}
	want := errors.New("storage")
	h.Downloads = &downloadServiceFake{err: want}
	if _, err = h.CreateDownload(context.Background(), gen.CreateDownloadRequestObject{Body: &gen.CreateDownloadRequest{Kind: gen.CreateDownloadRequestKindTray}}); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	h.Downloads = &downloadServiceFake{err: mediaapp.ErrNoFlickrKey}
	got, err = h.CreateDownload(context.Background(), gen.CreateDownloadRequestObject{Body: &gen.CreateDownloadRequest{Kind: gen.CreateDownloadRequestKindTray}})
	if err != nil {
		t.Fatal(err)
	}
	response, ok := got.(gen.CreateDownload409JSONResponse)
	if !ok || response.Code != "flickr_key_required" || response.Message == "" {
		t.Fatalf("missing key response=%#v", got)
	}
	h.Downloads = &downloadServiceFake{err: mediaapp.ErrTempStorageFull}
	got, err = h.CreateDownload(context.Background(), gen.CreateDownloadRequestObject{Body: &gen.CreateDownloadRequest{Kind: gen.CreateDownloadRequestKindTray}})
	if err != nil {
		t.Fatal(err)
	}
	response, ok = got.(gen.CreateDownload409JSONResponse)
	if !ok || response.Code != "temp_storage_full" {
		t.Fatalf("storage pressure response=%#v", got)
	}
}

func TestGetDownloadPartsRequiresOwnerAndMapsErrors(t *testing.T) {
	request := gen.GetDownloadPartsRequestObject{ID: 9}
	if got, _ := (DownloadHandler{}).GetDownloadParts(context.Background(), request); got.(gen.GetDownloadParts401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("missing principal response = %#v", got)
	}
	h := DownloadHandler{Principal: func(context.Context) (int64, bool) { return 0, false }, Parts: &downloadPartsServiceFake{}}
	if got, _ := h.GetDownloadParts(context.Background(), request); got.(gen.GetDownloadParts401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("invalid principal response = %#v", got)
	}

	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	parts := &downloadPartsServiceFake{links: []mediaapp.DownloadPartLink{{Number: 2, URL: "https://storage.invalid/part", Bytes: 42, ExpiresAt: now}}}
	h = DownloadHandler{Principal: func(context.Context) (int64, bool) { return 7, true }, Parts: parts}
	got, err := h.GetDownloadParts(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	response := got.(gen.GetDownloadParts200JSONResponse)
	if response.DownloadID != 9 || len(response.Parts) != 1 || response.Parts[0].PartNumber != 2 || response.Parts[0].Bytes != 42 || parts.userID != 7 || parts.download != 9 {
		t.Fatalf("response=%+v owner=%d download=%d", response, parts.userID, parts.download)
	}

	parts.getErr = mediaapp.ErrDownloadNotFound
	got, err = h.GetDownloadParts(context.Background(), request)
	if err != nil || got.(gen.GetDownloadParts404JSONResponse).Code != "download_not_found" {
		t.Fatalf("not found response=%#v err=%v", got, err)
	}
	parts.getErr = errors.New("database")
	if _, err = h.GetDownloadParts(context.Background(), request); !errors.Is(err, parts.getErr) {
		t.Fatalf("internal error = %v", err)
	}
}

func TestMarkDownloadPartFetchedAndRetryFailedMapResults(t *testing.T) {
	fetchedRequest := gen.MarkDownloadPartFetchedRequestObject{ID: 9, PartNumber: 3}
	if got, _ := (DownloadHandler{}).MarkDownloadPartFetched(context.Background(), fetchedRequest); got.(gen.MarkDownloadPartFetched401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("missing principal response = %#v", got)
	}
	principal := func(context.Context) (int64, bool) { return 7, true }
	parts := &downloadPartsServiceFake{}
	h := DownloadHandler{Principal: principal, Parts: parts}
	got, err := h.MarkDownloadPartFetched(context.Background(), fetchedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(gen.MarkDownloadPartFetched204Response); !ok || parts.userID != 7 || parts.download != 9 || len(parts.fetched) != 1 || parts.fetched[0] != 3 {
		t.Fatalf("response=%#v owner=%d download=%d fetched=%v", got, parts.userID, parts.download, parts.fetched)
	}
	parts.fetchErr = mediaapp.ErrDownloadNotFound
	got, err = h.MarkDownloadPartFetched(context.Background(), fetchedRequest)
	if err != nil || got.(gen.MarkDownloadPartFetched404JSONResponse).Code != "download_not_found" {
		t.Fatalf("not found response=%#v err=%v", got, err)
	}
	parts.fetchErr = errors.New("database")
	if _, err := h.MarkDownloadPartFetched(context.Background(), fetchedRequest); !errors.Is(err, parts.fetchErr) {
		t.Fatalf("internal error = %v", err)
	}

	retryRequest := gen.RetryFailedDownloadRequestObject{ID: 9}
	if got, _ := (DownloadHandler{}).RetryFailedDownload(context.Background(), retryRequest); got.(gen.RetryFailedDownload401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("missing principal response = %#v", got)
	}
	retry := &retryDownloadServiceFake{count: 2}
	h = DownloadHandler{Principal: principal, Retry: retry}
	gotRetry, err := h.RetryFailedDownload(context.Background(), retryRequest)
	if err != nil || gotRetry.(gen.RetryFailedDownload200JSONResponse).Requeued != 2 || retry.userID != 7 || retry.download != 9 {
		t.Fatalf("retry response=%#v owner=%d download=%d err=%v", gotRetry, retry.userID, retry.download, err)
	}
	retry.err = mediaapp.ErrDownloadNotFound
	gotRetry, err = h.RetryFailedDownload(context.Background(), retryRequest)
	if err != nil || gotRetry.(gen.RetryFailedDownload404JSONResponse).Code != "download_not_found" {
		t.Fatalf("retry not found response=%#v err=%v", gotRetry, err)
	}
	retry.err = errors.New("queue")
	if _, err := h.RetryFailedDownload(context.Background(), retryRequest); !errors.Is(err, retry.err) {
		t.Fatalf("retry internal error = %v", err)
	}
}
