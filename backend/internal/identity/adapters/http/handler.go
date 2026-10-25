package http

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		return registerValidationResponse("required", "required", "required", ""), nil
	}
	if request.Body.Password == nil {
		return registerValidationResponse("", "required", "", ""), nil
	}
	if !bool(request.Body.AcceptTerms) {
		return registerValidationResponse("", "", "required", ""), nil
	}
	u, err := h.RegisterUseCase.Execute(ctx, string(request.Body.Email), *request.Body.Password, string(request.Body.Locale))
	if err != nil {
		if errors.Is(err, app.ErrInvalidInput) {
			return registerValidationResponseFor(request.Body), nil
		}
		if errors.Is(err, app.ErrRegistrationClosed) {
			return gen.Register409JSONResponse{Code: gen.RegistrationClosed, Message: "Registration is closed"}, nil
		}
		if errors.Is(err, app.ErrEmailExists) {
			return gen.Register409JSONResponse{Code: gen.EmailTaken, Message: "Email is already registered"}, nil
		}
		return nil, err
	}
	return gen.Register201JSONResponse(apiUser(u)), nil
}

func registerValidationResponse(email, password, terms, locale string) gen.Register400JSONResponse {
	response := gen.Register400JSONResponse{Code: gen.ValidationFailed, Message: "Registration details are invalid"}
	if email != "" {
		field := gen.RegisterValidationErrorDetailsFieldsEmail(email)
		response.Details.Fields.Email = &field
	}
	if password != "" {
		field := gen.RegisterValidationErrorDetailsFieldsPassword(password)
		response.Details.Fields.Password = &field
	}
	if terms != "" {
		field := gen.RegisterValidationErrorDetailsFieldsAcceptTerms(terms)
		response.Details.Fields.AcceptTerms = &field
	}
	if locale != "" {
		field := gen.RegisterValidationErrorDetailsFieldsLocale(locale)
		response.Details.Fields.Locale = &field
	}
	return response
}

func registerValidationResponseFor(body *gen.RegisterJSONRequestBody) gen.Register400JSONResponse {
	email := strings.TrimSpace(string(body.Email))
	password := ""
	if body.Password != nil {
		password = *body.Password
	}
	if email == "" {
		return registerValidationResponse("required", "", "", "")
	}
	if len(email) > app.MaxEmailBytes {
		return registerValidationResponse("too_long", "", "", "")
	}
	if password == "" {
		return registerValidationResponse("", "required", "", "")
	}
	if len(password) < app.MinPasswordBytes {
		return registerValidationResponse("", "too_short", "", "")
	}
	if len(password) > app.MaxPasswordBytes {
		return registerValidationResponse("", "too_long", "", "")
	}
	return registerValidationResponse("", "", "", "invalid")
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
	ip := clientIP(ctx)
	if request.Body.Password == nil {
		return gen.Login401Response{}, nil
	}
	u, err := h.LoginUseCase.Execute(ctx, string(request.Body.Email), *request.Body.Password, ip)
	if errors.Is(err, app.ErrRateLimited) {
		remaining, retryErr := h.LoginUseCase.RetryAfter(ctx, string(request.Body.Email), ip)
		if retryErr != nil || remaining <= 0 {
			remaining = time.Minute
		}
		retryAfter := int(math.Ceil(remaining.Seconds()))
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
			UserAgentLabel: userAgentLabel(item.UserAgent), IPMasked: maskIP(item.IPAddress),
			LastSeenAt: item.LastSeenAt, CreatedAt: item.CreatedAt,
		})
	}
	return gen.ListMySessions200JSONResponse{Items: result}, nil
}

func maskIP(value string) string {
	ip := net.ParseIP(value)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
	}
	return ip.Mask(net.CIDRMask(48, 128)).String()
}

func userAgentLabel(value string) string {
	value = strings.ToLower(value)
	if value == "" {
		return "Unknown device"
	}
	browser := firstUserAgentMatch(value, "Browser", [][2]string{{"edg/", "Edge"}, {"firefox/", "Firefox"}, {"chromium/", "Chromium"}, {"chrome/", "Chrome"}, {"safari/", "Safari"}})
	os := firstUserAgentMatch(value, "", [][2]string{{"windows", "Windows"}, {"android", "Android"}, {"iphone", "iOS"}, {"ipad", "iOS"}, {"ios", "iOS"}, {"mac os", "macOS"}, {"macintosh", "macOS"}, {"linux", "Linux"}})
	if os == "" {
		return browser
	}
	return browser + " on " + os
}

func firstUserAgentMatch(value, fallback string, candidates [][2]string) string {
	for _, candidate := range candidates {
		if !strings.Contains(value, candidate[0]) {
			continue
		}
		return candidate[1]
	}
	return fallback
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
		return gen.ChangeMyPassword400JSONResponse{Code: "validation_failed", Message: "Password is invalid"}, nil
	}
	if request.Body.CurrentPassword == nil || request.Body.NewPassword == nil {
		return gen.ChangeMyPassword400JSONResponse{Code: "validation_failed", Message: "Password is invalid"}, nil
	}
	err := h.Password.Execute(ctx, principal.User.ID, *request.Body.CurrentPassword, *request.Body.NewPassword, principal.TokenHash)
	if errors.Is(err, app.ErrUnauthenticated) {
		return gen.ChangeMyPassword401Response{}, nil
	}
	if errors.Is(err, app.ErrInvalidCurrentPassword) {
		return gen.ChangeMyPassword400JSONResponse{Code: "invalid_current_password", Message: "Current password is incorrect"}, nil
	}
	if errors.Is(err, app.ErrInvalidInput) {
		return gen.ChangeMyPassword400JSONResponse{Code: "validation_failed", Message: "Password is invalid"}, nil
	}
	if errors.Is(err, app.ErrRateLimited) {
		retryAfter, retryErr := h.Password.RetryAfter(ctx, principal.User.ID)
		if retryErr != nil {
			return nil, retryErr
		}
		seconds := int(math.Ceil(retryAfter.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		return gen.ChangeMyPassword429JSONResponse{
			Body: gen.LoginRateLimited{Code: gen.RateLimited, Message: "Too many password change attempts", Details: struct {
				RetryAfterSeconds int `json:"retry_after_seconds"`
			}{RetryAfterSeconds: seconds}},
			Headers: gen.ChangeMyPassword429ResponseHeaders{RetryAfter: &seconds},
		}, nil
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

func clientIP(ctx context.Context) string {
	value := requestIP(ctx)
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
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
