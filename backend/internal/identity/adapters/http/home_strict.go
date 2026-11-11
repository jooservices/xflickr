package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

var errHomeServedByHome = errors.New("identity: Home snapshot is served by the home module")

// GetMeHome leaves the Home snapshot implementation to the home module.
func (StrictHandler) GetMeHome(context.Context, gen.GetMeHomeRequestObject) (gen.GetMeHomeResponseObject, error) {
	return nil, errHomeServedByHome
}
