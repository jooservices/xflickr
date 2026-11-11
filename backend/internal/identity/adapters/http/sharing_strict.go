package http

import (
	"context"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func (StrictHandler) GetMeSharing(context.Context, gen.GetMeSharingRequestObject) (gen.GetMeSharingResponseObject, error) {
	return nil, errSharingServedBySharing
}
