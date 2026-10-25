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
