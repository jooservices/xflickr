package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// ConnectService is the Flickr account connection use case set.
type ConnectService interface {
	List(context.Context, int64) ([]domain.Account, error)
	Start(ctx context.Context, userID, keyID int64) (string, error)
	Complete(ctx context.Context, userID int64, state, oauthToken, verifier string) (domain.Account, error)
}

var _ ConnectService = app.Connect{}

// ListFlickrAccounts returns account metadata for the signed-in user.
func (h ConnectHandler) ListFlickrAccounts(ctx context.Context, _ gen.ListFlickrAccountsRequestObject) (gen.ListFlickrAccountsResponseObject, error) {
	userID, ok := principalID(ctx, h.Principal)
	if !ok {
		return gen.ListFlickrAccounts401JSONResponse(unauthenticated()), nil
	}
	if h.Accounts == nil {
		return nil, errors.New("accounts: connect service is required")
	}
	accounts, err := h.Accounts.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListFlickrAccounts200JSONResponse, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, gen.FlickrAccount{ID: account.ID, KeyID: account.KeyID, Nsid: account.NSID, Username: account.Username, Status: gen.FlickrAccountStatus(account.Status), CreatedAt: account.CreatedAt})
	}
	return out, nil
}

// ConnectHandler implements the /flickr-accounts operations on the generated
// strict server. Responses never include tokens or the request token secret.
type ConnectHandler struct {
	Accounts  ConnectService
	Principal Principal
}

// ConnectFlickrAccount starts the flow and returns the Flickr authorize URL.
func (h ConnectHandler) ConnectFlickrAccount(ctx context.Context, request gen.ConnectFlickrAccountRequestObject) (gen.ConnectFlickrAccountResponseObject, error) {
	userID, ok := principalID(ctx, h.Principal)
	if !ok {
		return gen.ConnectFlickrAccount401JSONResponse(unauthenticated()), nil
	}
	if request.Body == nil {
		return gen.ConnectFlickrAccount400JSONResponse(connectInvalidInput()), nil
	}
	if h.Accounts == nil {
		return nil, errors.New("accounts: connect service is required")
	}
	authorizeURL, err := h.Accounts.Start(ctx, userID, request.Body.KeyID)
	if err != nil {
		return connectFailure(err)
	}
	return gen.ConnectFlickrAccount200JSONResponse{AuthorizeURL: authorizeURL}, nil
}

// FlickrAccountCallback completes the flow for the signed-in user.
func (h ConnectHandler) FlickrAccountCallback(ctx context.Context, request gen.FlickrAccountCallbackRequestObject) (gen.FlickrAccountCallbackResponseObject, error) {
	userID, ok := principalID(ctx, h.Principal)
	if !ok {
		return gen.FlickrAccountCallback401JSONResponse(unauthenticated()), nil
	}
	if h.Accounts == nil {
		return nil, errors.New("accounts: connect service is required")
	}
	verifier := ""
	if request.Params.OauthVerifier != nil {
		verifier = *request.Params.OauthVerifier
	}
	account, err := h.Accounts.Complete(ctx, userID, request.Params.State, request.Params.OauthToken, verifier)
	if err != nil {
		return callbackFailure(err)
	}
	return gen.FlickrAccountCallback200JSONResponse{
		ID:        account.ID,
		KeyID:     account.KeyID,
		Nsid:      account.NSID,
		Username:  account.Username,
		Status:    gen.FlickrAccountStatus(account.Status),
		CreatedAt: account.CreatedAt,
	}, nil
}

func principalID(ctx context.Context, principal Principal) (int64, bool) {
	if principal == nil || ctx == nil {
		return 0, false
	}
	id, ok := principal(ctx)
	if !ok || id == 0 {
		return 0, false
	}
	return id, true
}

func connectFailure(err error) (gen.ConnectFlickrAccountResponseObject, error) {
	body, status, ok := connectError(err)
	if !ok {
		return nil, err
	}
	switch status {
	case http.StatusBadRequest:
		return gen.ConnectFlickrAccount400JSONResponse(body), nil
	case http.StatusNotFound:
		return gen.ConnectFlickrAccount404JSONResponse(body), nil
	case http.StatusConflict:
		return gen.ConnectFlickrAccount409JSONResponse(body), nil
	case http.StatusUnprocessableEntity:
		return gen.ConnectFlickrAccount422JSONResponse(body), nil
	case http.StatusServiceUnavailable:
		return gen.ConnectFlickrAccount503JSONResponse(body), nil
	default:
		return nil, err
	}
}

func callbackFailure(err error) (gen.FlickrAccountCallbackResponseObject, error) {
	body, status, ok := connectError(err)
	if !ok {
		return nil, err
	}
	switch status {
	case http.StatusBadRequest:
		return gen.FlickrAccountCallback400JSONResponse(body), nil
	case http.StatusConflict:
		return gen.FlickrAccountCallback409JSONResponse(body), nil
	case http.StatusUnprocessableEntity:
		return gen.FlickrAccountCallback422JSONResponse(body), nil
	case http.StatusServiceUnavailable:
		return gen.FlickrAccountCallback503JSONResponse(body), nil
	default:
		return nil, err
	}
}

func connectError(err error) (gen.Error, int, bool) {
	switch {
	case errors.Is(err, app.ErrInvalidInput):
		return connectInvalidInput(), http.StatusBadRequest, true
	case errors.Is(err, app.ErrInvalidState):
		return gen.Error{Code: "invalid_state", Message: "The authorization is unknown, expired, or already used"}, http.StatusBadRequest, true
	case errors.Is(err, app.ErrAuthorizationDenied):
		return gen.Error{Code: "authorization_denied", Message: "Access was not granted on Flickr"}, http.StatusBadRequest, true
	case errors.Is(err, app.ErrNotFound):
		return notFound(), http.StatusNotFound, true
	case errors.Is(err, app.ErrKeyNotUsable):
		return gen.Error{Code: "key_not_usable", Message: "The API key cannot authorize accounts"}, http.StatusConflict, true
	case errors.Is(err, app.ErrOAuthRejected):
		return gen.Error{Code: "oauth_rejected", Message: "Flickr rejected the authorization"}, http.StatusUnprocessableEntity, true
	case errors.Is(err, app.ErrOAuthUnavailable):
		return gen.Error{Code: "flickr_unavailable", Message: "Flickr could not be reached"}, http.StatusServiceUnavailable, true
	default:
		return gen.Error{}, 0, false
	}
}

func connectInvalidInput() gen.Error {
	return gen.Error{Code: "invalid_input", Message: "The request is invalid"}
}
