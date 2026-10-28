package http

import (
	"context"
	"errors"

	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type Principal func(context.Context) (int64, bool)

type DownloadService interface {
	Create(context.Context, mediaapp.CreateDownloadInput) (mediadomain.Download, error)
}

type DownloadPartService interface {
	Get(context.Context, int64, int64) ([]mediaapp.DownloadPartLink, error)
	MarkFetched(context.Context, int64, int64, int) error
}

type FailedDownloadRetryService interface {
	Run(context.Context, int64, int64) (int, error)
}

type DownloadHandler struct {
	Downloads DownloadService
	Parts     DownloadPartService
	Retry     FailedDownloadRetryService
	Principal Principal
}

var _ DownloadService = mediaapp.Downloads{}

func (h DownloadHandler) CreateDownload(ctx context.Context, request gen.CreateDownloadRequestObject) (gen.CreateDownloadResponseObject, error) {
	if h.Principal == nil {
		return gen.CreateDownload401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	userID, ok := h.Principal(ctx)
	if !ok {
		return gen.CreateDownload401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	if request.Body == nil || h.Downloads == nil {
		return gen.CreateDownload400JSONResponse(gen.Error{Code: "invalid_request"}), nil
	}
	input := mediaapp.CreateDownloadInput{UserID: userID, Kind: mediadomain.DownloadKind(request.Body.Kind)}
	if request.Body.Subject != nil {
		input.Subject = *request.Body.Subject
	}
	if request.Body.PhotoIds != nil {
		input.PhotoIDs = *request.Body.PhotoIds
	}
	created, err := h.Downloads.Create(ctx, input)
	if errors.Is(err, mediaapp.ErrInvalidDownloadRequest) || errors.Is(err, mediadomain.ErrInvalidDownload) {
		return gen.CreateDownload400JSONResponse(gen.Error{Code: "invalid_request"}), nil
	}
	if errors.Is(err, mediaapp.ErrNoFlickrKey) {
		return gen.CreateDownload409JSONResponse(gen.Error{Code: "flickr_key_required", Message: "Connect a Flickr account with a valid API key to confirm photo sizes"}), nil
	}
	if errors.Is(err, mediaapp.ErrTempStorageFull) {
		return gen.CreateDownload409JSONResponse(gen.Error{Code: "temp_storage_full", Message: "Temporary download storage is full"}), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.CreateDownload201JSONResponse(gen.Download{ID: created.ID, Kind: gen.DownloadKind(created.Kind), Subject: created.Subject, Status: created.Status, CreatedAt: created.CreatedAt, ExpiresAt: created.ExpiresAt, ItemCount: len(created.Items), Skipped: created.Skipped}), nil
}

func (h DownloadHandler) GetDownloadParts(ctx context.Context, request gen.GetDownloadPartsRequestObject) (gen.GetDownloadPartsResponseObject, error) {
	if h.Principal == nil {
		return gen.GetDownloadParts401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	userID, ok := h.Principal(ctx)
	if !ok {
		return gen.GetDownloadParts401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	if h.Parts == nil {
		return nil, errors.New("media: download part service is not configured")
	}
	links, err := h.Parts.Get(ctx, userID, request.ID)
	if errors.Is(err, mediaapp.ErrDownloadNotFound) {
		return gen.GetDownloadParts404JSONResponse(gen.Error{Code: "download_not_found"}), nil
	}
	if err != nil {
		return nil, err
	}
	response := gen.DownloadPartLinks{DownloadID: request.ID, Parts: make([]gen.DownloadPartLink, 0, len(links))}
	for _, link := range links {
		response.Parts = append(response.Parts, gen.DownloadPartLink{PartNumber: link.Number, URL: link.URL, Bytes: link.Bytes, ExpiresAt: link.ExpiresAt})
	}
	return gen.GetDownloadParts200JSONResponse(response), nil
}

func (h DownloadHandler) MarkDownloadPartFetched(ctx context.Context, request gen.MarkDownloadPartFetchedRequestObject) (gen.MarkDownloadPartFetchedResponseObject, error) {
	if h.Principal == nil {
		return gen.MarkDownloadPartFetched401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	userID, ok := h.Principal(ctx)
	if !ok {
		return gen.MarkDownloadPartFetched401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	if h.Parts == nil {
		return nil, errors.New("media: download part service is not configured")
	}
	if err := h.Parts.MarkFetched(ctx, userID, request.ID, request.PartNumber); errors.Is(err, mediaapp.ErrDownloadNotFound) {
		return gen.MarkDownloadPartFetched404JSONResponse(gen.Error{Code: "download_not_found"}), nil
	} else if err != nil {
		return nil, err
	}
	return gen.MarkDownloadPartFetched204Response{}, nil
}

func (h DownloadHandler) RetryFailedDownload(ctx context.Context, request gen.RetryFailedDownloadRequestObject) (gen.RetryFailedDownloadResponseObject, error) {
	if h.Principal == nil {
		return gen.RetryFailedDownload401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	userID, ok := h.Principal(ctx)
	if !ok {
		return gen.RetryFailedDownload401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	if h.Retry == nil {
		return nil, errors.New("media: failed download retry service is not configured")
	}
	requeued, err := h.Retry.Run(ctx, userID, request.ID)
	if errors.Is(err, mediaapp.ErrDownloadNotFound) {
		return gen.RetryFailedDownload404JSONResponse(gen.Error{Code: "download_not_found"}), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.RetryFailedDownload200JSONResponse(gen.RetryFailedDownloadResponse{Requeued: requeued}), nil
}
