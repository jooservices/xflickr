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

type DownloadHandler struct {
	Downloads DownloadService
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
	if err != nil {
		return nil, err
	}
	return gen.CreateDownload201JSONResponse(gen.Download{ID: created.ID, Kind: gen.DownloadKind(created.Kind), Subject: created.Subject, Status: created.Status, CreatedAt: created.CreatedAt, ExpiresAt: created.ExpiresAt, ItemCount: len(created.Items), Skipped: created.Skipped}), nil
}
