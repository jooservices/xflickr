package http

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func TestGetMeSharingUsesSharingRoute(t *testing.T) {
	response, err := (StrictHandler{}).GetMeSharing(context.Background(), gen.GetMeSharingRequestObject{})
	if response != nil || !errors.Is(err, errSharingServedBySharing) {
		t.Fatalf("GetMeSharing() = %T, %v", response, err)
	}
}
