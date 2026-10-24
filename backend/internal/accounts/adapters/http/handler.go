// Package http serves the API key endpoints. Responses mask the public key
// and never include the secret. This package does not log request bodies.
package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// Principal returns the signed-in user id. The composition root adapts the
// identity session principal so this package does not import identity.
type Principal func(context.Context) (int64, bool)

// KeyService is the API key use case set.
type KeyService interface {
	Create(context.Context, int64, app.CreateInput) (domain.APIKey, error)
	Update(context.Context, int64, int64, app.UpdateInput) (domain.APIKey, error)
	Delete(context.Context, int64, int64) error
	List(context.Context, int64) ([]domain.APIKey, error)
	Get(context.Context, int64, int64) (domain.APIKey, error)
}

// KeyHandler implements the /keys operations on the generated strict server.
type KeyHandler struct {
	Keys      KeyService
	Principal Principal
}

var _ KeyService = app.Keys{}

// ListKeys returns the signed-in user's keys. The secret is not selected.
func (h KeyHandler) ListKeys(ctx context.Context, _ gen.ListKeysRequestObject) (gen.ListKeysResponseObject, error) {
	userID, ok := h.userID(ctx)
	if !ok {
		return gen.ListKeys401JSONResponse(unauthenticated()), nil
	}
	if err := h.ready(); err != nil {
		return nil, err
	}
	keys, err := h.Keys.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	body := make(gen.ListKeys200JSONResponse, 0, len(keys))
	for _, key := range keys {
		body = append(body, toAPIKey(key))
	}
	return body, nil
}

// CreateKey checks the key, then stores it. A rejected key responds with
// key_invalid and the use case stores nothing.
func (h KeyHandler) CreateKey(ctx context.Context, request gen.CreateKeyRequestObject) (gen.CreateKeyResponseObject, error) {
	userID, ok := h.userID(ctx)
	if !ok {
		return gen.CreateKey401JSONResponse(unauthenticated()), nil
	}
	if request.Body == nil {
		return gen.CreateKey400JSONResponse(invalidInput()), nil
	}
	if err := h.ready(); err != nil {
		return nil, err
	}
	secret := ""
	if request.Body.Secret != nil {
		secret = *request.Body.Secret
	}
	created, err := h.Keys.Create(ctx, userID, app.CreateInput{
		Label:          request.Body.Label,
		Key:            request.Body.Key,
		Secret:         secret,
		PerHourLimit:   request.Body.PerHourLimit,
		PerSecondLimit: request.Body.PerSecondLimit,
		Burst:          request.Body.Burst,
	})
	if err != nil {
		return createFailure(err)
	}
	return gen.CreateKey201JSONResponse(toAPIKey(created)), nil
}

// DeleteKey removes the key. The use case publishes key.deleted.
func (h KeyHandler) DeleteKey(ctx context.Context, request gen.DeleteKeyRequestObject) (gen.DeleteKeyResponseObject, error) {
	userID, ok := h.userID(ctx)
	if !ok {
		return gen.DeleteKey401JSONResponse(unauthenticated()), nil
	}
	if err := h.ready(); err != nil {
		return nil, err
	}
	if err := h.Keys.Delete(ctx, userID, request.ID); err != nil {
		return deleteFailure(err)
	}
	return gen.DeleteKey204Response{}, nil
}

// GetKey returns one key owned by the signed-in user.
func (h KeyHandler) GetKey(ctx context.Context, request gen.GetKeyRequestObject) (gen.GetKeyResponseObject, error) {
	userID, ok := h.userID(ctx)
	if !ok {
		return gen.GetKey401JSONResponse(unauthenticated()), nil
	}
	if err := h.ready(); err != nil {
		return nil, err
	}
	key, err := h.Keys.Get(ctx, userID, request.ID)
	if err != nil {
		return getFailure(err)
	}
	return gen.GetKey200JSONResponse(toAPIKey(key)), nil
}

// UpdateKey changes the label, limits, or secret. A secret change is applied
// by the use case, which marks connected accounts reconnect_required.
func (h KeyHandler) UpdateKey(ctx context.Context, request gen.UpdateKeyRequestObject) (gen.UpdateKeyResponseObject, error) {
	userID, ok := h.userID(ctx)
	if !ok {
		return gen.UpdateKey401JSONResponse(unauthenticated()), nil
	}
	if request.Body == nil {
		return gen.UpdateKey400JSONResponse(invalidInput()), nil
	}
	if err := h.ready(); err != nil {
		return nil, err
	}
	updated, err := h.Keys.Update(ctx, userID, request.ID, app.UpdateInput{
		Label:          request.Body.Label,
		Secret:         request.Body.Secret,
		PerHourLimit:   request.Body.PerHourLimit,
		PerSecondLimit: request.Body.PerSecondLimit,
		Burst:          request.Body.Burst,
	})
	if err != nil {
		return updateFailure(err)
	}
	return gen.UpdateKey200JSONResponse(toAPIKey(updated)), nil
}

func (h KeyHandler) userID(ctx context.Context) (int64, bool) {
	if h.Principal == nil || ctx == nil {
		return 0, false
	}
	id, ok := h.Principal(ctx)
	if !ok || id == 0 {
		return 0, false
	}
	return id, true
}

func (h KeyHandler) ready() error {
	if h.Keys == nil {
		return errors.New("accounts: api key service is required")
	}
	return nil
}

func toAPIKey(key domain.APIKey) gen.APIKey {
	return gen.APIKey{
		ID:             key.ID,
		Label:          key.Label,
		KeyPublic:      key.MaskedPublicKey(),
		PerHourLimit:   key.PerHourLimit,
		PerSecondLimit: key.PerSecondLimit,
		Burst:          key.Burst,
		Status:         gen.APIKeyStatus(key.Status),
		CreatedAt:      key.CreatedAt,
	}
}

func createFailure(err error) (gen.CreateKeyResponseObject, error) {
	body, status, ok := apiError(err)
	if !ok {
		return nil, err
	}
	switch status {
	case http.StatusBadRequest:
		return gen.CreateKey400JSONResponse(body), nil
	case http.StatusConflict:
		return gen.CreateKey409JSONResponse(body), nil
	case http.StatusUnprocessableEntity:
		return gen.CreateKey422JSONResponse(body), nil
	case http.StatusServiceUnavailable:
		return gen.CreateKey503JSONResponse(body), nil
	default:
		return nil, err
	}
}

func updateFailure(err error) (gen.UpdateKeyResponseObject, error) {
	body, status, ok := apiError(err)
	if !ok {
		return nil, err
	}
	switch status {
	case http.StatusBadRequest:
		return gen.UpdateKey400JSONResponse(body), nil
	case http.StatusNotFound:
		return gen.UpdateKey404JSONResponse(body), nil
	case http.StatusConflict:
		return gen.UpdateKey409JSONResponse(body), nil
	case http.StatusUnprocessableEntity:
		return gen.UpdateKey422JSONResponse(body), nil
	default:
		return nil, err
	}
}

func deleteFailure(err error) (gen.DeleteKeyResponseObject, error) {
	body, status, ok := apiError(err)
	if !ok || status != http.StatusNotFound {
		return nil, err
	}
	return gen.DeleteKey404JSONResponse(body), nil
}

func getFailure(err error) (gen.GetKeyResponseObject, error) {
	body, status, ok := apiError(err)
	if !ok || status != http.StatusNotFound {
		return nil, err
	}
	return gen.GetKey404JSONResponse(body), nil
}

func apiError(err error) (gen.Error, int, bool) {
	switch {
	case errors.Is(err, app.ErrInvalidInput):
		return invalidInput(), http.StatusBadRequest, true
	case errors.Is(err, app.ErrKeyInvalid):
		return keyInvalid(), http.StatusUnprocessableEntity, true
	case errors.Is(err, domain.ErrLimitExceeded):
		return limitExceeded(), http.StatusUnprocessableEntity, true
	case errors.Is(err, app.ErrKeyExists):
		return keyExists(), http.StatusConflict, true
	case errors.Is(err, domain.ErrRevoked):
		return keyRevoked(), http.StatusConflict, true
	case errors.Is(err, app.ErrNotFound):
		return notFound(), http.StatusNotFound, true
	case errors.Is(err, app.ErrKeyCheckFailed):
		return keyCheckFailed(), http.StatusServiceUnavailable, true
	default:
		return gen.Error{}, 0, false
	}
}

func unauthenticated() gen.Error {
	return gen.Error{Code: "unauthenticated", Message: "Authentication required"}
}

func invalidInput() gen.Error {
	return gen.Error{Code: "invalid_input", Message: "The API key request is invalid"}
}

func keyInvalid() gen.Error {
	return gen.Error{Code: "key_invalid", Message: "The Flickr API key is invalid"}
}

func limitExceeded() gen.Error {
	return gen.Error{Code: "limit_exceeded", Message: "Limit exceeds the administrator cap"}
}

func keyExists() gen.Error {
	return gen.Error{Code: "key_exists", Message: "This API key is already saved"}
}

func keyRevoked() gen.Error {
	return gen.Error{Code: "key_revoked", Message: "Revoked API keys cannot be changed"}
}

func notFound() gen.Error {
	return gen.Error{Code: "not_found", Message: "API key not found"}
}

func keyCheckFailed() gen.Error {
	return gen.Error{Code: "key_check_failed", Message: "Flickr key check did not complete"}
}
