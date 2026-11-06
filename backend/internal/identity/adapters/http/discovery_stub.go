package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// GetAPIEntry is composed by the API role; the identity-only server has no
// knowledge of the other modules' available collections.
func (StrictHandler) GetAPIEntry(context.Context, gen.GetAPIEntryRequestObject) (gen.GetAPIEntryResponseObject, error) {
	return nil, errors.New("identity: discovery is served by the API role")
}
