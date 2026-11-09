package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"go.opentelemetry.io/otel/trace"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

func TestIdentityResponseVisitors(t *testing.T) {
	visitors := []struct {
		name string
		call func(*apiResponse, http.ResponseWriter) error
	}{
		{"authentication options", (*apiResponse).VisitGetAuthConfigResponse},
		{"registration", (*apiResponse).VisitRegisterResponse},
		{"login", (*apiResponse).VisitLoginResponse},
		{"logout", (*apiResponse).VisitLogoutResponse},
		{"revoke other sessions", (*apiResponse).VisitRevokeOtherSessionsResponse},
		{"get me", (*apiResponse).VisitGetMeResponse},
		{"update me", (*apiResponse).VisitUpdateMeResponse},
		{"list sessions", (*apiResponse).VisitListMySessionsResponse},
		{"revoke session", (*apiResponse).VisitRevokeMySessionResponse},
		{"change password", (*apiResponse).VisitChangeMyPasswordResponse},
		{"get user", (*apiResponse).VisitGetUserResponse},
		{"get current session", (*apiResponse).VisitGetCurrentSessionResponse},
		{"get session", (*apiResponse).VisitGetSessionResponse},
		{"get my session", (*apiResponse).VisitGetMySessionResponse},
	}
	for _, visitor := range visitors {
		t.Run(visitor.name, func(t *testing.T) {
			response := &apiResponse{status: http.StatusOK, message: "Success", path: "/api/v1/me", data: map[string]any{"id": 1}}
			recorder := httptest.NewRecorder()
			if err := visitor.call(response, recorder); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), "application/json") || recorder.Header().Get("Cache-Control") != privateNoStore {
				t.Fatalf("response status=%d headers=%v", recorder.Code, recorder.Header())
			}
		})
	}

	problem := &apiResponse{problem: rest.NewProblem("invalid-current-password", "Current password is incorrect", nil), path: "/api/v1/me/password", retryAfter: 12}
	recorder := httptest.NewRecorder()
	if err := problem.VisitChangeMyPasswordResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusUnprocessableEntity || recorder.Header().Get("Retry-After") != "12" || !strings.Contains(recorder.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("problem response status=%d headers=%v", recorder.Code, recorder.Header())
	}
	noContent := httptest.NewRecorder()
	if err := (&apiResponse{status: http.StatusNoContent}).VisitLogoutResponse(noContent); err != nil {
		t.Fatal(err)
	}
	if noContent.Code != http.StatusNoContent || noContent.Body.Len() != 0 {
		t.Fatalf("204 response = %d %q", noContent.Code, noContent.Body.String())
	}
	created := httptest.NewRecorder()
	if err := (&apiResponse{status: http.StatusCreated, message: "Created", path: "/api/v1/users", location: "/api/v1/users/1"}).VisitRegisterResponse(created); err != nil {
		t.Fatal(err)
	}
	if created.Code != http.StatusCreated || created.Header().Get("Location") != "/api/v1/users/1" {
		t.Fatalf("created response status=%d headers=%v", created.Code, created.Header())
	}
}

func TestIdentityRepresentationsAndTraceIDs(t *testing.T) {
	user := domain.User{ID: 4, Email: "u@example.test", Role: domain.RoleAdmin, Locale: "vi", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("offset", 3600))}
	public := publicUserRepresentation(user, "/api/v1/users/4")
	if _, exists := public["email"]; exists || public["id"] != int64(4) {
		t.Fatalf("public user projection = %#v", public)
	}
	session := domain.Session{ID: 8, UserID: 4, UserAgent: "Firefox Linux", IPAddress: "203.0.113.9", LastSeenAt: user.CreatedAt, CreatedAt: user.CreatedAt, ExpiresAt: user.CreatedAt.Add(time.Hour)}
	for _, current := range []bool{true, false} {
		value := sessionRepresentation(session, user, current, "/api/v1/me/sessions/8")
		links := value["links"].(rest.Links)
		if links["self"].Href != "/api/v1/me/sessions/8" || value["user"] == nil {
			t.Fatalf("session representation = %#v", value)
		}
	}
	if got := responseTraceID(context.Background()); got != "" {
		t.Fatalf("empty trace id = %q", got)
	}
	var traceID trace.TraceID
	traceID[0] = 1
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled}))
	if got := responseTraceID(ctx); got != traceID.String() {
		t.Fatalf("trace id = %q", got)
	}
}

func TestAuthOptionsRESTUsesRegistrationPolicyAndUserCount(t *testing.T) {
	for _, tc := range []struct {
		name      string
		exists    bool
		existsErr error
		wantOpen  bool
		wantErr   bool
	}{
		{name: "closed with existing users", exists: true},
		{name: "first user allowed", wantOpen: true},
		{name: "user lookup failure", existsErr: errors.New("database unavailable"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := Handler{RegisterUseCase: app.Register{Policy: openRegistration(false), Repository: repo{usersExist: tc.exists, existErr: tc.existsErr}}}
			response, err := handler.GetAuthConfig(context.Background(), gen.GetAuthConfigRequestObject{})
			if (err != nil) != tc.wantErr {
				t.Fatalf("GetAuthConfig() error = %v", err)
			}
			if tc.wantErr {
				return
			}
			value := response.(*apiResponse).data.(map[string]any)
			if value["registration_open"] != tc.wantOpen {
				t.Fatalf("registration_open = %v", value["registration_open"])
			}
		})
	}
}

func TestRegistrationFieldValidation(t *testing.T) {
	longEmail := strings.Repeat("a", app.MaxEmailBytes+1)
	longPassword := strings.Repeat("p", app.MaxPasswordBytes+1)
	valid := registerBody("u@example.test", stringPointer("password123"))
	for _, tc := range []struct {
		name string
		body *gen.RegisterJSONRequestBody
		want int
	}{
		{name: "valid", body: valid},
		{name: "blank email", body: &gen.RegisterRequest{Password: stringPointer("password123"), AcceptTerms: true, Locale: gen.RegisterRequestLocaleEn}, want: 1},
		{name: "long email", body: &gen.RegisterRequest{Email: openapiEmail(longEmail), Password: stringPointer("password123"), AcceptTerms: true, Locale: gen.RegisterRequestLocaleEn}, want: 1},
		{name: "long password", body: &gen.RegisterRequest{Email: openapiEmail("u@example.test"), Password: &longPassword, AcceptTerms: true, Locale: gen.RegisterRequestLocaleEn}, want: 1},
		{name: "unaccepted terms", body: &gen.RegisterRequest{Email: openapiEmail("u@example.test"), Password: stringPointer("password123"), Locale: gen.RegisterRequestLocaleEn}, want: 1},
		{name: "invalid locale", body: &gen.RegisterRequest{Email: openapiEmail("u@example.test"), Password: stringPointer("password123"), AcceptTerms: true, Locale: gen.RegisterRequestLocale("fr")}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := registrationFields(tc.body)
			if len(fields) != tc.want {
				t.Fatalf("registration fields = %#v", fields)
			}
		})
	}
}

func TestGetUserAndMeRESTHandlers(t *testing.T) {
	user := domain.User{ID: 5, Email: "u@example.test", Role: domain.RoleUser, Locale: "en", CreatedAt: time.Now().UTC()}
	handler := Handler{Users: repo{user: user}}
	ctx := WithPrincipal(context.Background(), Principal{User: user})
	response, err := handler.GetUser(ctx, gen.GetUserRequestObject{ID: 5})
	if err != nil || response.(*apiResponse).status != http.StatusOK {
		t.Fatalf("GetUser() = %T, %v", response, err)
	}
	for _, tc := range []struct {
		name  string
		id    int64
		owner int64
		repo  repo
	}{
		{name: "invalid id", owner: 5},
		{name: "missing", id: 8, owner: 8, repo: repo{err: app.ErrUserNotFound}},
		{name: "infrastructure", id: 9, owner: 9, repo: repo{err: errors.New("database")}},
	} {
		if tc.name == "infrastructure" {
			userContext := WithPrincipal(context.Background(), Principal{User: domain.User{ID: tc.owner, Role: domain.RoleUser}})
			if _, err := (Handler{Users: tc.repo}).GetUser(userContext, gen.GetUserRequestObject{ID: tc.id}); err == nil {
				t.Fatal("GetUser() swallowed repository error")
			}
			continue
		}
		userContext := WithPrincipal(context.Background(), Principal{User: domain.User{ID: tc.owner, Role: domain.RoleUser}})
		got, err := (Handler{Users: tc.repo}).GetUser(userContext, gen.GetUserRequestObject{ID: tc.id})
		if err != nil || got.(*apiResponse).problem.Status != http.StatusNotFound {
			t.Fatalf("%s response=%#v err=%v", tc.name, got, err)
		}
	}

	meContext := WithUserID(context.Background(), user.ID)
	me, err := handler.GetMe(meContext, gen.GetMeRequestObject{})
	if err != nil || me.(*apiResponse).status != http.StatusOK {
		t.Fatalf("GetMe() = %T, %v", me, err)
	}
	for _, tc := range []struct {
		name string
		repo repo
		id   int64
	}{
		{name: "missing principal", repo: repo{user: user}},
		{name: "disabled user", repo: repo{user: domain.User{ID: 5, Disabled: true}}, id: 5},
		{name: "missing user", repo: repo{err: app.ErrUserNotFound}, id: 5},
	} {
		response, err := (Handler{Users: tc.repo}).GetMe(WithUserID(context.Background(), tc.id), gen.GetMeRequestObject{})
		if err != nil || response.(*apiResponse).problem.Status != http.StatusUnauthorized {
			t.Fatalf("%s response=%#v err=%v", tc.name, response, err)
		}
	}
	if _, err := (Handler{Users: repo{err: errors.New("database")}}).GetMe(meContext, gen.GetMeRequestObject{}); err == nil {
		t.Fatal("GetMe() swallowed repository error")
	}
	if _, ok := userIDFromContext(WithPrincipal(context.Background(), Principal{User: user})); !ok {
		t.Fatal("principal user id was not found")
	}
}

type getUserLookup struct {
	user  domain.User
	err   error
	reads []int64
}

func (lookup *getUserLookup) FindByID(_ context.Context, id int64) (domain.User, error) {
	lookup.reads = append(lookup.reads, id)
	return lookup.user, lookup.err
}

func TestGetUserRequiresOwnerOrAdmin(t *testing.T) {
	target := domain.User{ID: 5, Email: "u@example.test", Role: domain.RoleUser}
	for _, tc := range []struct {
		name       string
		principal  domain.User
		wantStatus int
		wantReads  int
	}{
		{name: "anonymous", wantStatus: http.StatusUnauthorized},
		{name: "different user", principal: domain.User{ID: 8, Role: domain.RoleUser}, wantStatus: http.StatusNotFound},
		{name: "owner", principal: domain.User{ID: 5, Role: domain.RoleUser}, wantStatus: http.StatusOK, wantReads: 1},
		{name: "administrator", principal: domain.User{ID: 8, Role: domain.RoleAdmin}, wantStatus: http.StatusOK, wantReads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &getUserLookup{user: target}
			ctx := context.Background()
			if tc.principal.ID > 0 {
				ctx = WithPrincipal(ctx, Principal{User: tc.principal})
			}
			response, err := (Handler{Users: lookup}).GetUser(ctx, gen.GetUserRequestObject{ID: target.ID})
			if err != nil {
				t.Fatalf("GetUser() error = %v", err)
			}
			result := response.(*apiResponse)
			status := result.status
			if result.problem != nil {
				status = result.problem.Status
			}
			if status != tc.wantStatus {
				t.Fatalf("GetUser() status = %d, want %d", status, tc.wantStatus)
			}
			if len(lookup.reads) != tc.wantReads {
				t.Fatalf("FindByID calls = %v, want %d", lookup.reads, tc.wantReads)
			}
		})
	}
}

func TestIdentitySessionReadHandlers(t *testing.T) {
	user := domain.User{ID: 9, Email: "u@example.test", Role: domain.RoleUser, CreatedAt: time.Now().UTC()}
	current := domain.Session{ID: 31, UserID: user.ID, CreatedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	owned := domain.Session{ID: 32, UserID: user.ID, CreatedAt: current.CreatedAt, LastSeenAt: current.LastSeenAt, ExpiresAt: current.ExpiresAt}
	ctx := WithPrincipal(context.Background(), Principal{User: user, Session: current, TokenHash: []byte{1, 2, 3}})
	handler := Handler{Sessions: &fakeSessions{listed: []domain.Session{owned}}}
	currentResponse, err := handler.GetCurrentSession(ctx, gen.GetCurrentSessionRequestObject{})
	if err != nil || currentResponse.(*apiResponse).status != http.StatusOK {
		t.Fatalf("GetCurrentSession() = %T, %v", currentResponse, err)
	}
	for _, read := range []struct {
		name string
		call func(Handler, context.Context, int64) (any, error)
	}{
		{name: "session", call: func(h Handler, c context.Context, id int64) (any, error) {
			return h.GetSession(c, gen.GetSessionRequestObject{ID: id})
		}},
		{name: "collection member", call: func(h Handler, c context.Context, id int64) (any, error) {
			return h.GetMySession(c, gen.GetMySessionRequestObject{ID: id})
		}},
	} {
		t.Run(read.name, func(t *testing.T) {
			got, err := read.call(handler, ctx, 32)
			if err != nil || got.(*apiResponse).status != http.StatusOK {
				t.Fatalf("owned read = %#v, %v", got, err)
			}
			missing, err := read.call(handler, ctx, 44)
			if err != nil || missing.(*apiResponse).problem.Status != http.StatusNotFound {
				t.Fatalf("missing read = %#v, %v", missing, err)
			}
			invalid, err := read.call(handler, ctx, 0)
			if err != nil || invalid.(*apiResponse).problem.Status != http.StatusNotFound {
				t.Fatalf("invalid id read = %#v, %v", invalid, err)
			}
			anonymous, err := read.call(handler, context.Background(), 32)
			if err != nil || anonymous.(*apiResponse).problem.Status != http.StatusUnauthorized {
				t.Fatalf("anonymous read = %#v, %v", anonymous, err)
			}
		})
	}
	if _, err := handler.GetCurrentSession(context.Background(), gen.GetCurrentSessionRequestObject{}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Handler{}).GetSession(ctx, gen.GetSessionRequestObject{ID: 32}); err == nil {
		t.Fatal("GetSession() with no session service succeeded")
	}
	if _, err := (Handler{Sessions: &fakeSessions{findErr: app.ErrUnauthenticated}}).GetSession(ctx, gen.GetSessionRequestObject{ID: 32}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Handler{Sessions: &fakeSessions{findErr: errors.New("database")}}).GetSession(ctx, gen.GetSessionRequestObject{ID: 32}); err == nil {
		t.Fatal("GetSession() swallowed repository error")
	}
}

func TestListMySessionsCursorAndFailures(t *testing.T) {
	ctx := sessionPrincipalContext()
	items := []domain.Session{
		{ID: 9, UserID: 9, LastSeenAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)},
		{ID: 8, UserID: 9, LastSeenAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		{ID: 7, UserID: 9, LastSeenAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	limit := 2
	got, err := (Handler{Sessions: &fakeSessions{listed: items}}).ListMySessions(ctx, gen.ListMySessionsRequestObject{Params: gen.ListMySessionsParams{Limit: &limit}})
	if err != nil {
		t.Fatal(err)
	}
	response := got.(*apiResponse)
	if response.status != http.StatusOK || len(response.data.([]map[string]any)) != 2 {
		t.Fatalf("page = %#v", response)
	}
	pagination := response.meta["pagination"].(rest.Pagination)
	if !pagination.HasMore || pagination.NextCursor == "" {
		t.Fatalf("pagination = %#v", pagination)
	}
	links := response.meta["links"].(map[string]string)
	if links["next"] == "" || links["self"] == "" {
		t.Fatalf("page links = %#v", links)
	}

	validCursorBody, err := json.Marshal(app.SessionCursor{LastSeenAt: items[1].LastSeenAt, ID: items[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	validCursor := rest.EncodeCursor(validCursorBody)
	query := "cursor"
	if _, err := (Handler{Sessions: &fakeSessions{listed: items}}).ListMySessions(ctx, gen.ListMySessionsRequestObject{Params: gen.ListMySessionsParams{Cursor: &validCursor}}); err != nil {
		t.Fatal(err)
	}
	invalid, err := (Handler{Sessions: &fakeSessions{listed: items}}).ListMySessions(ctx, gen.ListMySessionsRequestObject{Params: gen.ListMySessionsParams{Cursor: &query}})
	if err != nil || invalid.(*apiResponse).problem == nil {
		t.Fatalf("invalid cursor response = %#v, %v", invalid, err)
	}
	invalidJSONCursor := rest.EncodeCursor([]byte("{}"))
	invalid, err = (Handler{Sessions: &fakeSessions{listed: items}}).ListMySessions(ctx, gen.ListMySessionsRequestObject{Params: gen.ListMySessionsParams{Cursor: &invalidJSONCursor}})
	if err != nil || invalid.(*apiResponse).problem == nil {
		t.Fatalf("cursor with missing fields response = %#v, %v", invalid, err)
	}
	badLimit := 101
	invalid, err = (Handler{Sessions: &fakeSessions{listed: items}}).ListMySessions(ctx, gen.ListMySessionsRequestObject{Params: gen.ListMySessionsParams{Limit: &badLimit}})
	if err != nil || invalid.(*apiResponse).problem == nil {
		t.Fatalf("limit above maximum response = %#v, %v", invalid, err)
	}
	if _, err := (Handler{}).ListMySessions(ctx, gen.ListMySessionsRequestObject{}); err == nil {
		t.Fatal("missing session service succeeded")
	}
	if _, err := (Handler{Sessions: &fakeSessions{pageErr: app.ErrUnauthenticated}}).ListMySessions(ctx, gen.ListMySessionsRequestObject{}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Handler{Sessions: &fakeSessions{pageErr: errors.New("database")}}).ListMySessions(ctx, gen.ListMySessionsRequestObject{}); err == nil {
		t.Fatal("session page error was swallowed")
	}
	if _, err := (Handler{Sessions: &fakeSessions{listed: items}}).ListMySessions(context.Background(), gen.ListMySessionsRequestObject{}); err != nil {
		t.Fatal(err)
	}
}

func TestIdentitySessionRevocationAndPasswordBranches(t *testing.T) {
	ctx := sessionPrincipalContext()
	for _, tc := range []struct {
		name    string
		scope   gen.RevokeOtherSessionsParamsScope
		fake    *fakeSessions
		ctx     context.Context
		want    int
		wantErr bool
	}{
		{name: "success", scope: gen.Others, fake: &fakeSessions{}, ctx: ctx, want: http.StatusNoContent},
		{name: "invalid scope", scope: "all", fake: &fakeSessions{}, ctx: ctx, want: http.StatusUnprocessableEntity},
		{name: "anonymous", scope: gen.Others, fake: &fakeSessions{}, ctx: context.Background(), want: http.StatusUnauthorized},
		{name: "missing service", scope: gen.Others, ctx: ctx, wantErr: true},
		{name: "session already absent", scope: gen.Others, fake: &fakeSessions{revokeOthersErr: app.ErrUnauthenticated}, ctx: ctx, want: http.StatusUnauthorized},
		{name: "storage error", scope: gen.Others, fake: &fakeSessions{revokeOthersErr: errors.New("database")}, ctx: ctx, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var service SessionService
			if tc.fake != nil {
				service = tc.fake
			}
			response, err := (Handler{Sessions: service}).RevokeOtherSessions(tc.ctx, gen.RevokeOtherSessionsRequestObject{Params: gen.RevokeOtherSessionsParams{Scope: tc.scope}})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if tc.wantErr {
				return
			}
			value := response.(*apiResponse)
			status := value.status
			if value.problem != nil {
				status = value.problem.Status
			}
			if status != tc.want {
				t.Fatalf("status = %d, want %d", status, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name    string
		id      int64
		fake    *fakeSessions
		want    int
		wantErr bool
	}{
		{name: "missing", id: 44, fake: &fakeSessions{}, want: http.StatusNotFound},
		{name: "unauthenticated", id: 44, fake: &fakeSessions{revokeByIDErr: app.ErrUnauthenticated}, want: http.StatusUnauthorized},
		{name: "storage error", id: 44, fake: &fakeSessions{revokeByIDErr: errors.New("database")}, wantErr: true},
		{name: "missing service", id: 44, wantErr: true},
	} {
		t.Run("single revoke "+tc.name, func(t *testing.T) {
			var service SessionService
			if tc.fake != nil {
				service = tc.fake
			}
			response, err := (Handler{Sessions: service}).RevokeMySession(ctx, gen.RevokeMySessionRequestObject{ID: tc.id})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if !tc.wantErr {
				if response.(*apiResponse).problem == nil || response.(*apiResponse).problem.Status != tc.want {
					t.Fatalf("response = %#v", response)
				}
			}
		})
	}

	current, next := "current-password", "replacement-password"
	passwordBody := &gen.ChangePasswordRequest{CurrentPassword: &current, NewPassword: &next}
	params := domain.Argon2Params{Memory: 8, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	hash, err := domain.HashPassword(current, params)
	if err != nil {
		t.Fatal(err)
	}
	passwordHandler := Handler{Password: app.ChangePassword{Repository: repo{user: domain.User{ID: 9, PasswordHash: hash}}, Sessions: passwordSessions{}, Params: params}}
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		body    *gen.ChangePasswordRequest
		handler Handler
		want    int
		wantErr bool
	}{
		{name: "success", ctx: ctx, body: passwordBody, handler: passwordHandler, want: http.StatusNoContent},
		{name: "missing body", ctx: ctx, handler: passwordHandler, want: http.StatusBadRequest},
		{name: "invalid new password", ctx: ctx, body: &gen.ChangePasswordRequest{CurrentPassword: &current, NewPassword: stringPointer("short")}, handler: passwordHandler, want: http.StatusUnprocessableEntity},
		{name: "infrastructure", ctx: ctx, body: passwordBody, handler: Handler{Password: app.ChangePassword{Repository: repo{user: domain.User{ID: 9, PasswordHash: hash}, err: errors.New("database")}, Sessions: passwordSessions{}, Params: params}}, wantErr: true},
		{name: "anonymous", ctx: context.Background(), body: passwordBody, handler: passwordHandler, want: http.StatusUnauthorized},
	} {
		t.Run("password "+tc.name, func(t *testing.T) {
			response, err := tc.handler.ChangeMyPassword(tc.ctx, gen.ChangeMyPasswordRequestObject{Body: tc.body})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if !tc.wantErr {
				value := response.(*apiResponse)
				status := value.status
				if value.problem != nil {
					status = value.problem.Status
				}
				if status != tc.want {
					t.Fatalf("status = %d, want %d", status, tc.want)
				}
			}
		})
	}
}

func sessionPrincipalContext() context.Context {
	user := domain.User{ID: 9, Email: "u@example.test", Role: domain.RoleUser}
	session := domain.Session{ID: 9, UserID: user.ID, LastSeenAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	return WithPrincipal(context.Background(), Principal{User: user, Session: session, TokenHash: []byte{1, 2, 3}})
}

func openapiEmail(value string) openapi_types.Email { return openapi_types.Email(value) }

func stringPointer(value string) *string { return &value }
