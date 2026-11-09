package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

const privateNoStore = "private, no-store"

type apiResponse struct {
	status      int
	message     string
	path        string
	traceID     string
	location    string
	retryAfter  int
	data        any
	meta        map[string]any
	problem     *rest.Problem
	sessionUser *domain.User
}

func (response *apiResponse) write(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", privateNoStore)
	if response.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(response.retryAfter))
	}
	if response.problem != nil {
		return rest.WriteProblemAt(w, response.path, response.problem, response.traceID)
	}
	if response.status == http.StatusNoContent {
		rest.NoContent(w)
		return nil
	}
	if response.location != "" {
		w.Header().Set("Location", response.location)
	}
	return rest.WriteSuccess(w, response.status, response.message, response.data, response.meta, response.traceID)
}

func (response *apiResponse) VisitGetAuthConfigResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitRegisterResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitLoginResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitLogoutResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitRevokeOtherSessionsResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetMeResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitUpdateMeResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitListMySessionsResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitRevokeMySessionResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitChangeMyPasswordResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetUserResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetCurrentSessionResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetSessionResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) VisitGetMySessionResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response *apiResponse) issueSessionUserID() int64 {
	if response.sessionUser == nil {
		return 0
	}
	return response.sessionUser.ID
}

func (response *apiResponse) setIssuedSession(session domain.Session) {
	if response.sessionUser != nil {
		response.data = sessionRepresentation(session, *response.sessionUser, true, "")
	}
}

func responseTraceID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		return span.TraceID().String()
	}
	return ""
}

func identitySuccess(ctx context.Context, status int, path, message string, data any, meta map[string]any) *apiResponse {
	return &apiResponse{status: status, path: path, message: message, data: data, meta: meta, traceID: responseTraceID(ctx)}
}

func identityProblem(ctx context.Context, path, slug, detail string, fields rest.FieldErrors) *apiResponse {
	return &apiResponse{path: path, traceID: responseTraceID(ctx), problem: rest.NewProblem(slug, detail, fields)}
}

func authOptionsLinks() rest.Links {
	return rest.Links{
		"self":     rest.NewLink("/api/v1/authentication-options", http.MethodGet),
		"users":    rest.NewLink("/api/v1/users", http.MethodPost),
		"sessions": rest.NewLink("/api/v1/sessions", http.MethodPost),
	}
}

func userLinks(self string) rest.Links {
	return rest.Links{
		"self":     rest.NewLink(self, http.MethodGet),
		"me":       rest.NewLink("/api/v1/me", http.MethodGet),
		"sessions": rest.NewLink("/api/v1/sessions", http.MethodPost),
	}
}

func meLinks() rest.Links {
	return rest.Links{
		"self":                  rest.NewLink("/api/v1/me", http.MethodGet),
		"update":                rest.NewLink("/api/v1/me", http.MethodPatch),
		"sessions":              rest.NewLink("/api/v1/me/sessions", http.MethodGet),
		"revoke-other-sessions": rest.NewLink("/api/v1/me/sessions?scope=others", http.MethodDelete),
		"password":              rest.NewLink("/api/v1/me/password", http.MethodPut),
		"sharing":               rest.NewLink("/api/v1/me/sharing", http.MethodGet),
		"update-sharing":        rest.NewLink("/api/v1/me/sharing", http.MethodPut),
	}
}

func userRepresentation(user domain.User, links rest.Links) map[string]any {
	return map[string]any{
		"id": user.ID, "email": user.Email, "role": string(user.Role),
		"locale": user.Locale, "created_at": user.CreatedAt.UTC(), "links": links,
	}
}

func publicUserRepresentation(user domain.User, self string) map[string]any {
	return map[string]any{
		"id": user.ID, "role": string(user.Role), "locale": user.Locale,
		"created_at": user.CreatedAt.UTC(), "links": rest.Links{"self": rest.NewLink(self, http.MethodGet)},
	}
}

func sessionLinks(session domain.Session, current bool, href string) rest.Links {
	links := rest.Links{
		"self":    rest.NewLink(href, http.MethodGet),
		"session": rest.NewLink("/api/v1/sessions/"+strconv.FormatInt(session.ID, 10), http.MethodGet),
		"user":    rest.NewLink("/api/v1/users/"+strconv.FormatInt(session.UserID, 10), http.MethodGet),
	}
	if current {
		links["revoke"] = rest.NewLink("/api/v1/sessions/current", http.MethodDelete)
	} else {
		links["revoke"] = rest.NewLink("/api/v1/me/sessions/"+strconv.FormatInt(session.ID, 10), http.MethodDelete)
	}
	return links
}

func sessionSummary(session domain.Session, current bool) map[string]any {
	href := "/api/v1/me/sessions/" + strconv.FormatInt(session.ID, 10)
	return map[string]any{
		"id": strconv.FormatInt(session.ID, 10), "current": current,
		"user_agent_label": userAgentLabel(session.UserAgent), "ip_masked": maskIP(session.IPAddress),
		"last_seen_at": session.LastSeenAt.UTC(), "created_at": session.CreatedAt.UTC(),
		"expires_at": session.ExpiresAt.UTC(), "links": sessionLinks(session, current, href),
	}
}

func sessionRepresentation(session domain.Session, user domain.User, current bool, selfPath string) map[string]any {
	href := "/api/v1/sessions/" + strconv.FormatInt(session.ID, 10)
	if current {
		href = "/api/v1/sessions/current"
	}
	if selfPath != "" {
		href = selfPath
	}
	return map[string]any{
		"id": strconv.FormatInt(session.ID, 10), "current": current,
		"user_agent_label": userAgentLabel(session.UserAgent), "ip_masked": maskIP(session.IPAddress),
		"last_seen_at": session.LastSeenAt.UTC(), "created_at": session.CreatedAt.UTC(),
		"expires_at": session.ExpiresAt.UTC(), "links": sessionLinks(session, current, href),
		"user": userRepresentation(user, meLinks()),
	}
}

func (h Handler) GetAuthConfig(ctx context.Context, _ gen.GetAuthConfigRequestObject) (gen.GetAuthConfigResponseObject, error) {
	path := "/api/v1/authentication-options"
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
	data := map[string]any{"registration_open": open, "passkey_enabled": false, "password_min": app.MinPasswordBytes, "password_max": app.MaxPasswordBytes, "links": authOptionsLinks()}
	return identitySuccess(ctx, http.StatusOK, path, "Success", data, nil), nil
}

func (h Handler) Register(ctx context.Context, request gen.RegisterRequestObject) (gen.RegisterResponseObject, error) {
	path := "/api/v1/users"
	if request.Body == nil {
		return identityProblem(ctx, path, "malformed-request", "A registration body is required", nil), nil
	}
	fields := registrationFields(request.Body)
	if len(fields) > 0 {
		return identityProblem(ctx, path, "validation-error", "Registration details are invalid", fields), nil
	}
	u, err := h.RegisterUseCase.Execute(ctx, string(request.Body.Email), *request.Body.Password, string(request.Body.Locale))
	if errors.Is(err, app.ErrInvalidInput) {
		return identityProblem(ctx, path, "validation-error", "Registration details are invalid", registrationFields(request.Body)), nil
	}
	if errors.Is(err, app.ErrRegistrationClosed) {
		return identityProblem(ctx, path, "registration-closed", "Registration is closed", nil), nil
	}
	if errors.Is(err, app.ErrEmailExists) {
		return identityProblem(ctx, path, "email-taken", "Email is already registered", rest.FieldErrors{"email": {"Email is already registered"}}), nil
	}
	if err != nil {
		return nil, err
	}
	resource := "/api/v1/users/" + strconv.FormatInt(u.ID, 10)
	response := identitySuccess(ctx, http.StatusCreated, path, "Created", userRepresentation(u, userLinks(resource)), nil)
	response.location = resource
	return response, nil
}

func registrationFields(body *gen.RegisterJSONRequestBody) rest.FieldErrors {
	fields := rest.FieldErrors{}
	if strings.TrimSpace(string(body.Email)) == "" {
		fields["email"] = []string{"Email is required"}
	} else if len(strings.TrimSpace(string(body.Email))) > app.MaxEmailBytes {
		fields["email"] = []string{"Email is too long"}
	}
	if body.Password == nil || len(*body.Password) < app.MinPasswordBytes {
		fields["password"] = []string{"Password must contain at least 8 bytes"}
	} else if len(*body.Password) > app.MaxPasswordBytes {
		fields["password"] = []string{"Password is too long"}
	}
	if !bool(body.AcceptTerms) {
		fields["accept_terms"] = []string{"Terms must be accepted"}
	}
	if body.Locale != gen.RegisterRequestLocaleEn && body.Locale != gen.RegisterRequestLocaleVi {
		fields["locale"] = []string{"Locale must be en or vi"}
	}
	return fields
}

func (h Handler) Login(ctx context.Context, request gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	path := "/api/v1/sessions"
	if request.Body == nil || request.Body.Password == nil {
		return identityProblem(ctx, path, "malformed-request", "An email and password are required", nil), nil
	}
	ip := clientIP(ctx)
	u, err := h.LoginUseCase.Execute(ctx, string(request.Body.Email), *request.Body.Password, ip)
	if errors.Is(err, app.ErrRateLimited) {
		remaining, retryErr := h.LoginUseCase.RetryAfter(ctx, string(request.Body.Email), ip)
		if retryErr != nil {
			return nil, retryErr
		}
		retryAfter := int(math.Ceil(remaining.Seconds()))
		if retryAfter < 1 {
			retryAfter = 60
		}
		response := identityProblem(ctx, path, "account-locked", "Too many login attempts", nil)
		response.retryAfter = retryAfter
		return response, nil
	}
	if errors.Is(err, app.ErrAccountDisabled) {
		return identityProblem(ctx, path, "forbidden", "This account is disabled", nil), nil
	}
	if errors.Is(err, app.ErrInvalidCredentials) {
		return identityProblem(ctx, path, "unauthenticated", "Email or password is incorrect", nil), nil
	}
	if err != nil {
		return nil, err
	}
	response := identitySuccess(ctx, http.StatusCreated, path, "Created", nil, nil)
	response.location = "/api/v1/sessions/current"
	response.sessionUser = &u
	response.data = map[string]any{"user": userRepresentation(u, meLinks()), "links": sessionLinks(domain.Session{UserID: u.ID}, true, "/api/v1/sessions/current")}
	return response, nil
}

func (h Handler) Logout(context.Context, gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	return &apiResponse{status: http.StatusNoContent, path: "/api/v1/sessions/current"}, nil
}

func (h Handler) RevokeOtherSessions(ctx context.Context, request gen.RevokeOtherSessionsRequestObject) (gen.RevokeOtherSessionsResponseObject, error) {
	path := "/api/v1/me/sessions"
	if request.Params.Scope != gen.Others {
		return identityProblem(ctx, path, "validation-error", "The only supported scope is others", rest.FieldErrors{"scope": {"Must be others"}}), nil
	}
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 || len(principal.TokenHash) == 0 {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if h.Sessions == nil {
		return nil, errors.New("identity: session service is required")
	}
	if err := h.Sessions.RevokeOthers(ctx, principal.User.ID, principal.TokenHash); err != nil {
		if errors.Is(err, app.ErrUnauthenticated) {
			return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
		}
		return nil, err
	}
	return &apiResponse{status: http.StatusNoContent, path: path}, nil
}

func (h Handler) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	return h.getMe(ctx, "/api/v1/me")
}

func (h Handler) UpdateMe(ctx context.Context, request gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	path := "/api/v1/me"
	id, ok := userIDFromContext(ctx)
	if !ok || h.Users == nil {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if request.Body == nil {
		return identityProblem(ctx, path, "malformed-request", "A merge-patch object is required", nil), nil
	}
	if request.Body.Locale == nil {
		return h.getMe(ctx, path)
	}
	user, err := h.Profile.UpdateLocale(ctx, id, string(*request.Body.Locale))
	if errors.Is(err, app.ErrInvalidLocale) {
		return identityProblem(ctx, path, "validation-error", "Locale is invalid", rest.FieldErrors{"locale": {"Locale must be en or vi"}}), nil
	}
	if errors.Is(err, app.ErrUserNotFound) {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if err != nil {
		return nil, err
	}
	data := userRepresentation(user, meLinks())
	return identitySuccess(ctx, http.StatusOK, path, "Success", data, nil), nil
}

func (h Handler) getMe(ctx context.Context, path string) (*apiResponse, error) {
	id, ok := userIDFromContext(ctx)
	if !ok || h.Users == nil {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	user, err := h.Users.FindByID(ctx, id)
	if errors.Is(err, app.ErrUserNotFound) || user.Disabled {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if err != nil {
		return nil, err
	}
	return identitySuccess(ctx, http.StatusOK, path, "Success", userRepresentation(user, meLinks()), nil), nil
}

func userIDFromContext(ctx context.Context) (int64, bool) {
	if principal, ok := PrincipalFrom(ctx); ok && principal.User.ID > 0 {
		return principal.User.ID, true
	}
	id, ok := ctx.Value(contextKey{}).(int64)
	return id, ok && id > 0
}

func (h Handler) GetUser(ctx context.Context, request gen.GetUserRequestObject) (gen.GetUserResponseObject, error) {
	path := "/api/v1/users/" + strconv.FormatInt(request.ID, 10)
	if request.ID <= 0 {
		return identityProblem(ctx, path, "not-found", "User not found", nil), nil
	}
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID <= 0 {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if principal.User.ID != request.ID && principal.User.Role != domain.RoleAdmin {
		return identityProblem(ctx, path, "not-found", "User not found", nil), nil
	}
	if h.Users == nil {
		return identityProblem(ctx, path, "not-found", "User not found", nil), nil
	}
	user, err := h.Users.FindByID(ctx, request.ID)
	if errors.Is(err, app.ErrUserNotFound) {
		return identityProblem(ctx, path, "not-found", "User not found", nil), nil
	}
	if err != nil {
		return nil, err
	}
	return identitySuccess(ctx, http.StatusOK, path, "Success", publicUserRepresentation(user, path), nil), nil
}

func (h Handler) GetCurrentSession(ctx context.Context, _ gen.GetCurrentSessionRequestObject) (gen.GetCurrentSessionResponseObject, error) {
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 || principal.Session.ID <= 0 {
		return identityProblem(ctx, "/api/v1/sessions/current", "unauthenticated", "Authentication required", nil), nil
	}
	return identitySuccess(ctx, http.StatusOK, "/api/v1/sessions/current", "Success", sessionRepresentation(principal.Session, principal.User, true, ""), nil), nil
}

func (h Handler) GetSession(ctx context.Context, request gen.GetSessionRequestObject) (gen.GetSessionResponseObject, error) {
	return h.getOwnedSession(ctx, request.ID, "/api/v1/sessions/"+strconv.FormatInt(request.ID, 10))
}

func (h Handler) GetMySession(ctx context.Context, request gen.GetMySessionRequestObject) (gen.GetMySessionResponseObject, error) {
	return h.getOwnedSession(ctx, request.ID, "/api/v1/me/sessions/"+strconv.FormatInt(request.ID, 10))
}

func (h Handler) getOwnedSession(ctx context.Context, id int64, path string) (*apiResponse, error) {
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if id <= 0 {
		return identityProblem(ctx, path, "not-found", "Session not found", nil), nil
	}
	if id == principal.Session.ID && id > 0 {
		return identitySuccess(ctx, http.StatusOK, path, "Success", sessionRepresentation(principal.Session, principal.User, true, path), nil), nil
	}
	if h.Sessions == nil {
		return nil, errors.New("identity: session service is required")
	}
	item, err := h.Sessions.FindByID(ctx, principal.User.ID, id)
	if err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			return identityProblem(ctx, path, "not-found", "Session not found", nil), nil
		}
		if errors.Is(err, app.ErrUnauthenticated) {
			return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
		}
		return nil, err
	}
	return identitySuccess(ctx, http.StatusOK, path, "Success", sessionRepresentation(item, principal.User, false, path), nil), nil
}

func (h Handler) ListMySessions(ctx context.Context, request gen.ListMySessionsRequestObject) (gen.ListMySessionsResponseObject, error) {
	path := "/api/v1/me/sessions"
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	limit, invalid := parseSessionLimit(ctx, path, request.Params.Limit)
	if invalid != nil {
		return invalid, nil
	}
	cursor, invalid := parseSessionCursor(ctx, path, request.Params.Cursor)
	if invalid != nil {
		return invalid, nil
	}
	if h.Sessions == nil {
		return nil, errors.New("identity: session service is required")
	}
	items, err := h.Sessions.ListPage(ctx, principal.User.ID, cursor, limit+1)
	if errors.Is(err, app.ErrUnauthenticated) {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if err != nil {
		return nil, err
	}
	data, nextCursor, err := sessionSummaries(items, principal.Session.ID, limit)
	if err != nil {
		return nil, err
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}}
	if request.Params.Cursor != nil && *request.Params.Cursor != "" {
		values.Set("cursor", *request.Params.Cursor)
	}
	self := &url.URL{Path: path, RawQuery: values.Encode()}
	return identitySuccess(ctx, http.StatusOK, path, "Success", data, rest.PageMeta(self, limit, nextCursor)), nil
}

func parseSessionLimit(ctx context.Context, path string, requested *int) (int, *apiResponse) {
	limit := rest.DefaultLimit
	if requested != nil {
		parsed, err := rest.ParseLimit(strconv.Itoa(*requested))
		if err != nil {
			return 0, identityProblemFromError(ctx, path, err)
		}
		limit = parsed
	}
	return limit, nil
}

func parseSessionCursor(ctx context.Context, path string, requested *string) (*app.SessionCursor, *apiResponse) {
	var cursor *app.SessionCursor
	if requested != nil && *requested != "" {
		raw, err := rest.DecodeCursor(*requested)
		if err != nil {
			return nil, identityProblemFromError(ctx, path, err)
		}
		var decoded app.SessionCursor
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded.ID <= 0 || decoded.LastSeenAt.IsZero() {
			return nil, identityProblem(ctx, path, "validation-error", "Invalid cursor", rest.FieldErrors{"cursor": {"Invalid cursor"}})
		}
		cursor = &decoded
	}
	return cursor, nil
}

func sessionSummaries(items []domain.Session, currentID int64, limit int) ([]map[string]any, string, error) {
	page := items
	hasMore := len(page) > limit
	if hasMore {
		page = page[:limit]
	}
	data := make([]map[string]any, 0, len(page))
	for _, item := range page {
		data = append(data, sessionSummary(item, item.ID == currentID))
	}
	nextCursor := ""
	if hasMore && len(page) > 0 {
		last := page[len(page)-1]
		encoded, err := json.Marshal(app.SessionCursor{LastSeenAt: last.LastSeenAt, ID: last.ID})
		if err != nil {
			return nil, "", fmt.Errorf("identity: encode session cursor: %w", err)
		}
		nextCursor = rest.EncodeCursor(encoded)
	}
	return data, nextCursor, nil
}

func identityProblemFromError(ctx context.Context, path string, err error) *apiResponse {
	var problem *rest.Problem
	if errors.As(err, &problem) {
		return &apiResponse{path: path, traceID: responseTraceID(ctx), problem: problem}
	}
	return identityProblem(ctx, path, "validation-error", "Request values are invalid", nil)
}

func (h Handler) RevokeMySession(ctx context.Context, request gen.RevokeMySessionRequestObject) (gen.RevokeMySessionResponseObject, error) {
	path := "/api/v1/me/sessions/" + strconv.FormatInt(request.ID, 10)
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if request.ID == principal.Session.ID {
		return identityProblem(ctx, path, "not-found", "Session not found", nil), nil
	}
	if h.Sessions == nil {
		return nil, errors.New("identity: session service is required")
	}
	deleted, err := h.Sessions.RevokeByID(ctx, principal.User.ID, request.ID)
	if err != nil {
		if errors.Is(err, app.ErrUnauthenticated) {
			return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
		}
		return nil, err
	}
	if !deleted {
		return identityProblem(ctx, path, "not-found", "Session not found", nil), nil
	}
	return &apiResponse{status: http.StatusNoContent, path: path}, nil
}

func (h Handler) ChangeMyPassword(ctx context.Context, request gen.ChangeMyPasswordRequestObject) (gen.ChangeMyPasswordResponseObject, error) {
	path := "/api/v1/me/password"
	principal, ok := PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if request.Body == nil || request.Body.CurrentPassword == nil || request.Body.NewPassword == nil {
		return identityProblem(ctx, path, "malformed-request", "Both current_password and new_password are required", nil), nil
	}
	err := h.Password.Execute(ctx, principal.User.ID, *request.Body.CurrentPassword, *request.Body.NewPassword, principal.TokenHash)
	if errors.Is(err, app.ErrUnauthenticated) {
		return identityProblem(ctx, path, "unauthenticated", "Authentication required", nil), nil
	}
	if errors.Is(err, app.ErrInvalidCurrentPassword) {
		return identityProblem(ctx, path, "invalid-current-password", "Current password is incorrect", rest.FieldErrors{"current_password": {"Current password is incorrect"}}), nil
	}
	if errors.Is(err, app.ErrInvalidInput) {
		return identityProblem(ctx, path, "validation-error", "New password is invalid", rest.FieldErrors{"new_password": {"Password must contain between 8 and 128 bytes"}}), nil
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
		response := identityProblem(ctx, path, "rate-limited", "Too many password change attempts", nil)
		response.retryAfter = seconds
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return &apiResponse{status: http.StatusNoContent, path: path}, nil
}

func (h Handler) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{Status: "ok", Version: h.Version}, nil
}
