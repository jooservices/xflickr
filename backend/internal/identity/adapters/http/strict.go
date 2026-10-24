package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// StrictHandler lets identity tests construct the generated server. API key
// methods are implemented by the accounts module and composed in the API role.
type StrictHandler struct {
	Handler
}

var errAPIKeysServedByAccounts = errors.New("identity: api keys are served by the accounts module")

func (StrictHandler) ListKeys(context.Context, gen.ListKeysRequestObject) (gen.ListKeysResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

func (StrictHandler) CreateKey(context.Context, gen.CreateKeyRequestObject) (gen.CreateKeyResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

func (StrictHandler) DeleteKey(context.Context, gen.DeleteKeyRequestObject) (gen.DeleteKeyResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

func (StrictHandler) GetKey(context.Context, gen.GetKeyRequestObject) (gen.GetKeyResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}

func (StrictHandler) UpdateKey(context.Context, gen.UpdateKeyRequestObject) (gen.UpdateKeyResponseObject, error) {
	return nil, errAPIKeysServedByAccounts
}
