package http

import (
	"context"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func TestDiscoveryComposedByRole(t *testing.T) {
	response, err := (StrictHandler{}).GetAPIEntry(context.Background(), gen.GetAPIEntryRequestObject{})
	if response != nil || err == nil {
		t.Fatal(response, err)
	}
}
