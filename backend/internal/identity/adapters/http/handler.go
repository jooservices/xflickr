package http

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

type contextKey struct{}

func WithUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

type UserLookup interface {
	FindByID(context.Context, int64) (domain.User, error)
}
type Handler struct {
	RegisterUseCase app.Register
	LoginUseCase    app.Login
	Users           UserLookup
	Sessions        SessionService
	Version         string
}

func (h Handler) Register(ctx context.Context, request gen.RegisterRequestObject) (gen.RegisterResponseObject, error) {
	if request.Body == nil {
		return gen.Register400Response{}, nil
	}
	if request.Body.Password == nil {
		return gen.Register400Response{}, nil
	}
	u, err := h.RegisterUseCase.Execute(ctx, string(request.Body.Email), *request.Body.Password, "")
	if err != nil {
		if errors.Is(err, app.ErrInvalidInput) {
			return gen.Register400Response{}, nil
		}
		if errors.Is(err, app.ErrRegistrationClosed) || errors.Is(err, app.ErrEmailExists) {
			return gen.Register409Response{}, nil
		}
		return nil, err
	}
	return gen.Register201JSONResponse(apiUser(u)), nil
}
func (h Handler) Login(ctx context.Context, request gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	if request.Body == nil {
		return gen.Login401Response{}, nil
	}
	ip, _, _ := net.SplitHostPort(requestIP(ctx))
	if ip == "" {
		ip = requestIP(ctx)
	}
	if request.Body.Password == nil {
		return gen.Login401Response{}, nil
	}
	u, err := h.LoginUseCase.Execute(ctx, string(request.Body.Email), *request.Body.Password, ip)
	if errors.Is(err, app.ErrRateLimited) {
		return gen.Login429Response{}, nil
	}
	if errors.Is(err, app.ErrInvalidCredentials) {
		return gen.Login401Response{}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.Login200JSONResponse(apiUser(u)), nil
}
func (h Handler) Logout(context.Context, gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	return gen.Logout204Response{}, nil
}

func (h Handler) RevokeOtherSessions(ctx context.Context, _ gen.RevokeOtherSessionsRequestObject) (gen.RevokeOtherSessionsResponseObject, error) {
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 || len(principal.TokenHash) == 0 {
		return gen.RevokeOtherSessions401Response{}, nil
	}
	if h.Sessions == nil {
		return nil, errors.New("identity: session service is required")
	}
	if err := h.Sessions.RevokeOthers(ctx, principal.User.ID, principal.TokenHash); err != nil {
		if errors.Is(err, app.ErrUnauthenticated) {
			return gen.RevokeOtherSessions401Response{}, nil
		}
		return nil, err
	}
	return gen.RevokeOtherSessions204Response{}, nil
}
func (h Handler) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{Status: "ok", Version: h.Version}, nil
}
func (h Handler) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	id, ok := ctx.Value(contextKey{}).(int64)
	if !ok || id == 0 || h.Users == nil {
		return gen.GetMe401Response{}, nil
	}
	u, err := h.Users.FindByID(ctx, id)
	if err != nil || u.Disabled {
		return gen.GetMe401Response{}, nil
	}
	return gen.GetMe200JSONResponse(apiUser(u)), nil
}

// Remote address is attached by the HTTP adapter middleware to avoid trusting forwarded headers.
type remoteAddressKey struct{}

func WithRemoteAddress(ctx context.Context, address string) context.Context {
	return context.WithValue(ctx, remoteAddressKey{}, address)
}
func requestIP(ctx context.Context) string {
	value, _ := ctx.Value(remoteAddressKey{}).(string)
	return value
}
func Middleware(next gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		ctx = WithRemoteAddress(ctx, r.RemoteAddr)
		return next(ctx, w, r, request)
	}
}

// RequestBodyLimit caps JSON request bodies before the generated handler
// decodes them, preventing oversized credentials from reaching Argon2.
func RequestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		next.ServeHTTP(w, r)
	})
}

func apiUser(u domain.User) gen.User {
	return gen.User{ID: u.ID, Email: openapi_types.Email(u.Email), Role: gen.UserRole(u.Role), Locale: u.Locale, CreatedAt: u.CreatedAt}
}
