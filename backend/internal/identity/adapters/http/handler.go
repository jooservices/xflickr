package http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
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
	AdminUsers      AdminUserService
	Profile         app.UpdateProfile
	Password        app.ChangePassword
	Version         string
}

func (h Handler) GetAuthConfig(ctx context.Context, _ gen.GetAuthConfigRequestObject) (gen.GetAuthConfigResponseObject, error) {
	if h.RegisterUseCase.Policy == nil {
		return nil, errors.New("identity: registration policy is required")
	}
	open, err := h.RegisterUseCase.Policy.RegistrationOpen(ctx)
	if err != nil {
		return nil, err
	}
	if !open && h.RegisterUseCase.Repository != nil {
		exists, err := h.RegisterUseCase.Repository.UsersExist(ctx)
		if err != nil {
			return nil, err
		}
		open = !exists
	}
	return gen.GetAuthConfig200JSONResponse(gen.AuthConfig{
		RegistrationOpen: open,
		PasskeyEnabled:   false,
		PasswordMin:      app.MinPasswordBytes,
		PasswordMax:      app.MaxPasswordBytes,
	}), nil
}

func (h Handler) Register(ctx context.Context, request gen.RegisterRequestObject) (gen.RegisterResponseObject, error) {
	if request.Body == nil {
		return gen.Register400Response{}, nil
	}
	if request.Body.Password == nil {
		return gen.Register400Response{}, nil
	}
	if !bool(request.Body.AcceptTerms) {
		return gen.Register400Response{}, nil
	}
	u, err := h.RegisterUseCase.Execute(ctx, string(request.Body.Email), *request.Body.Password, string(request.Body.Locale))
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

func (h Handler) UpdateMe(ctx context.Context, request gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	id, ok := ctx.Value(contextKey{}).(int64)
	if !ok || id == 0 {
		return gen.UpdateMe401Response{}, nil
	}
	if request.Body == nil {
		return gen.UpdateMe400Response{}, nil
	}
	user, err := h.Profile.UpdateLocale(ctx, id, string(request.Body.Locale))
	if errors.Is(err, app.ErrInvalidLocale) {
		return gen.UpdateMe400Response{}, nil
	}
	if errors.Is(err, app.ErrUserNotFound) {
		return gen.UpdateMe401Response{}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.UpdateMe200JSONResponse(apiUser(user)), nil
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
		retryAfter := 900
		return gen.Login429JSONResponse{Body: gen.LoginRateLimited{Code: "rate_limited", Message: "Too many login attempts", Details: struct {
			RetryAfterSeconds int `json:"retry_after_seconds"`
		}{RetryAfterSeconds: retryAfter}}, Headers: gen.Login429ResponseHeaders{RetryAfter: &retryAfter}}, nil
	}
	if errors.Is(err, app.ErrAccountDisabled) {
		return gen.Login403Response{}, nil
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

func (h Handler) ListMySessions(ctx context.Context, _ gen.ListMySessionsRequestObject) (gen.ListMySessionsResponseObject, error) {
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return gen.ListMySessions401Response{}, nil
	}
	items, err := h.Sessions.List(ctx, principal.User.ID)
	if err != nil {
		if errors.Is(err, app.ErrUnauthenticated) {
			return gen.ListMySessions401Response{}, nil
		}
		return nil, err
	}
	result := make([]gen.Session, 0, len(items))
	for _, item := range items {
		result = append(result, gen.Session{
			ID: strconv.FormatInt(item.ID, 10), Current: item.ID == principal.Session.ID,
			UserAgentLabel: "Browser session", IPMasked: "",
			LastSeenAt: item.LastSeenAt, CreatedAt: item.CreatedAt,
		})
	}
	return gen.ListMySessions200JSONResponse{Items: result}, nil
}

func (h Handler) RevokeMySession(ctx context.Context, request gen.RevokeMySessionRequestObject) (gen.RevokeMySessionResponseObject, error) {
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return gen.RevokeMySession401Response{}, nil
	}
	if request.ID == principal.Session.ID {
		return gen.RevokeMySession404Response{}, nil
	}
	deleted, err := h.Sessions.RevokeByID(ctx, principal.User.ID, request.ID)
	if err != nil {
		if errors.Is(err, app.ErrUnauthenticated) {
			return gen.RevokeMySession401Response{}, nil
		}
		return nil, err
	}
	if !deleted {
		return gen.RevokeMySession404Response{}, nil
	}
	return gen.RevokeMySession204Response{}, nil
}

func (h Handler) ChangeMyPassword(ctx context.Context, request gen.ChangeMyPasswordRequestObject) (gen.ChangeMyPasswordResponseObject, error) {
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return gen.ChangeMyPassword401Response{}, nil
	}
	if request.Body == nil {
		return gen.ChangeMyPassword400Response{}, nil
	}
	if request.Body.CurrentPassword == nil || request.Body.NewPassword == nil {
		return gen.ChangeMyPassword400Response{}, nil
	}
	err := h.Password.Execute(ctx, principal.User.ID, *request.Body.CurrentPassword, *request.Body.NewPassword, principal.TokenHash)
	if errors.Is(err, app.ErrInvalidCurrentPassword) || errors.Is(err, app.ErrUnauthenticated) {
		return gen.ChangeMyPassword401Response{}, nil
	}
	if errors.Is(err, app.ErrInvalidInput) {
		return gen.ChangeMyPassword400Response{}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.ChangeMyPassword204Response{}, nil
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

func Middleware(next gen.StrictHandlerFunc, _ string) gen.StrictHandlerFunc {
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
