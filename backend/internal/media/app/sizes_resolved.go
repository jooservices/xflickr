package app

import (
	"context"
	"encoding/json"
	"errors"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
)

// SizesResolvedHandler continues downloads without coupling crawl to media.
type SizesResolvedHandler struct {
	Repository interface {
		ResolveSizes(context.Context, int64, string) error
	}
}

func (h SizesResolvedHandler) Handle(ctx context.Context, payload []byte) error {
	if ctx == nil || h.Repository == nil {
		return errors.New("media: sizes continuation repository and context are required")
	}
	var event catalogapp.SizesResolved
	if err := json.Unmarshal(payload, &event); err != nil || event.PhotoID <= 0 || (event.Status != "confirmed" && event.Status != "unavailable") {
		return errors.New("media: invalid sizes resolved event")
	}
	return h.Repository.ResolveSizes(ctx, event.PhotoID, event.Status)
}
