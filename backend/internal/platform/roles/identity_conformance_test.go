package roles

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

type identityJourneyRepository struct {
	mu     sync.Mutex
	nextID int64
	users  map[int64]domain.User
	byMail map[string]int64
}

func newIdentityJourneyRepository() *identityJourneyRepository {
	return &identityJourneyRepository{nextID: 1, users: map[int64]domain.User{}, byMail: map[string]int64{}}
}

func (r *identityJourneyRepository) Register(_ context.Context, user domain.User, _ bool) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byMail[user.Email]; exists {
		return domain.User{}, identityapp.ErrEmailExists
	}
	user.ID, user.Role, user.CreatedAt = r.nextID, domain.RoleUser, time.Now().UTC()
	r.nextID++
	r.users[user.ID], r.byMail[user.Email] = user, user.ID
	return user, nil
}

func (r *identityJourneyRepository) UsersExist(context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.users) > 0, nil
}

func (r *identityJourneyRepository) FindByEmail(_ context.Context, email string) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byMail[email]
	if !ok {
		return domain.User{}, identityapp.ErrUserNotFound
	}
	return r.users[id], nil
}

func (r *identityJourneyRepository) FindByID(_ context.Context, id int64) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.users[id]
	if !ok {
		return domain.User{}, identityapp.ErrUserNotFound
	}
	return user, nil
}

func (r *identityJourneyRepository) UpdateLocale(_ context.Context, id int64, locale string) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.users[id]
	if !ok {
		return domain.User{}, identityapp.ErrUserNotFound
	}
	user.Locale = locale
	r.users[id] = user
	return user, nil
}

func (r *identityJourneyRepository) UpdatePassword(_ context.Context, id int64, passwordHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.users[id]
	if !ok {
		return identityapp.ErrUserNotFound
	}
	user.PasswordHash = passwordHash
	r.users[id] = user
	return nil
}

type identityJourneyLimiter struct{}

func (identityJourneyLimiter) Allow(context.Context, string, string) (bool, error) { return true, nil }

func (identityJourneyLimiter) RetryAfter(context.Context, string, string) (time.Duration, error) {
	return 0, nil
}

type identityJourneySessions struct {
	mu       sync.Mutex
	users    *identityJourneyRepository
	nextID   int64
	sessions map[int64]domain.Session
	raw      map[string]int64
}

func newIdentityJourneySessions(users *identityJourneyRepository) *identityJourneySessions {
	return &identityJourneySessions{users: users, nextID: 1, sessions: map[int64]domain.Session{}, raw: map[string]int64{}}
}

func (s *identityJourneySessions) Issue(_ context.Context, userID int64, agent, ip string) (string, domain.Session, string, error) {
	raw, err := domain.NewOpaqueToken()
	if err != nil {
		return "", domain.Session{}, "", err
	}
	csrf, err := domain.NewOpaqueToken()
	if err != nil {
		return "", domain.Session{}, "", err
	}
	now := time.Now().UTC()
	session := domain.Session{ID: s.nextID, UserID: userID, UserAgent: agent, IPAddress: ip, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(domain.SessionLifetime)}
	s.mu.Lock()
	s.nextID++
	s.sessions[session.ID], s.raw[raw] = session, session.ID
	s.mu.Unlock()
	return raw, session, csrf, nil
}

func (s *identityJourneySessions) Authenticate(ctx context.Context, raw string) (identityapp.Authenticated, error) {
	s.mu.Lock()
	id, ok := s.raw[raw]
	session := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return identityapp.Authenticated{}, identityapp.ErrUnauthenticated
	}
	user, err := s.users.FindByID(ctx, session.UserID)
	if err != nil {
		return identityapp.Authenticated{}, identityapp.ErrUnauthenticated
	}
	return identityapp.Authenticated{User: user, Session: session, TokenHash: domain.HashSessionToken(raw)}, nil
}

func (s *identityJourneySessions) Revoke(_ context.Context, raw string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.raw[raw]
	delete(s.raw, raw)
	delete(s.sessions, id)
	return nil
}

func (s *identityJourneySessions) RevokeOthers(ctx context.Context, userID int64, hash []byte) error {
	return s.DeleteOtherSessions(ctx, userID, hash)
}

func (s *identityJourneySessions) List(_ context.Context, userID int64) ([]domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]domain.Session, 0)
	for _, session := range s.sessions {
		if session.UserID == userID {
			items = append(items, session)
		}
	}
	return items, nil
}

func (s *identityJourneySessions) ListPage(ctx context.Context, userID int64, cursor *identityapp.SessionCursor, limit int) ([]domain.Session, error) {
	items, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			if items[j].LastSeenAt.After(items[i].LastSeenAt) || items[j].LastSeenAt.Equal(items[i].LastSeenAt) && items[j].ID > items[i].ID {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	page := make([]domain.Session, 0, limit)
	for _, item := range items {
		if cursor != nil {
			afterCursor := item.LastSeenAt.After(cursor.LastSeenAt) || item.LastSeenAt.Equal(cursor.LastSeenAt) && item.ID >= cursor.ID
			if afterCursor {
				continue
			}
		}
		page = append(page, item)
		if len(page) == limit {
			break
		}
	}
	return page, nil
}

func (s *identityJourneySessions) FindByID(_ context.Context, userID, sessionID int64) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[sessionID]
	if !ok || item.UserID != userID {
		return domain.Session{}, identityapp.ErrSessionNotFound
	}
	return item, nil
}

func (s *identityJourneySessions) RevokeByID(_ context.Context, userID, sessionID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.UserID != userID {
		return false, nil
	}
	delete(s.sessions, sessionID)
	for raw, id := range s.raw {
		if id == sessionID {
			delete(s.raw, raw)
		}
	}
	return true, nil
}

func (s *identityJourneySessions) InsertSession(_ context.Context, userID int64, _ []byte, expires, now time.Time, agent, ip string) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := domain.Session{ID: s.nextID, UserID: userID, ExpiresAt: expires, LastSeenAt: now, CreatedAt: now, UserAgent: agent, IPAddress: ip}
	s.nextID++
	s.sessions[session.ID] = session
	return session, nil
}

func (s *identityJourneySessions) FindSessionByHash(context.Context, []byte, time.Time) (domain.Session, error) {
	return domain.Session{}, identityapp.ErrUnauthenticated
}

func (s *identityJourneySessions) TouchSession(context.Context, []byte, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (s *identityJourneySessions) DeleteOtherSessions(_ context.Context, userID int64, tokenHash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		if session.UserID != userID {
			continue
		}
		keep := false
		for raw, rawID := range s.raw {
			if rawID == id && subtle.ConstantTimeCompare(domain.HashSessionToken(raw), tokenHash) == 1 {
				keep = true
			}
		}
		if !keep {
			delete(s.sessions, id)
			for raw, rawID := range s.raw {
				if rawID == id {
					delete(s.raw, raw)
				}
			}
		}
	}
	return nil
}

func (s *identityJourneySessions) DeleteSessionByHash(context.Context, []byte) error { return nil }

func (s *identityJourneySessions) ListSessions(ctx context.Context, userID int64) ([]domain.Session, error) {
	return s.List(ctx, userID)
}

func (s *identityJourneySessions) ListSessionsPage(ctx context.Context, userID int64, cursor *identityapp.SessionCursor, limit int) ([]domain.Session, error) {
	return s.ListPage(ctx, userID, cursor, limit)
}

func (s *identityJourneySessions) FindSessionByID(ctx context.Context, userID, sessionID int64) (domain.Session, error) {
	return s.FindByID(ctx, userID, sessionID)
}

func (s *identityJourneySessions) DeleteSessionByID(ctx context.Context, userID, sessionID int64) (bool, error) {
	return s.RevokeByID(ctx, userID, sessionID)
}

type identityJourneyRegistration bool

func (p identityJourneyRegistration) RegistrationOpen(context.Context) (bool, error) {
	return bool(p), nil
}

type identityJourneySharing struct {
	mu      sync.Mutex
	setting sharingdomain.Setting
}

func (s *identityJourneySharing) Set(_ context.Context, setting sharingdomain.Setting) (sharingdomain.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setting.ChangedAt = time.Now().UTC()
	s.setting = setting
	return setting, nil
}

func (s *identityJourneySharing) Get(_ context.Context, userID int64) (sharingdomain.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setting.UserID != userID {
		return sharingdomain.Setting{UserID: userID}, nil
	}
	return s.setting, nil
}

func (s *identityJourneySharing) CountShared(context.Context, int64) (int64, error) {
	return 0, nil
}

type identityJourneyEnvelope struct {
	Success bool            `json:"success"`
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Meta    json.RawMessage `json:"meta"`
}

func TestIdentityLinkRelationsResolve(t *testing.T) {
	users := newIdentityJourneyRepository()
	sessions := newIdentityJourneySessions(users)
	params := domain.Argon2Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	dummyHash, err := domain.HashPassword("dummy-password", params)
	if err != nil {
		t.Fatal(err)
	}
	limiter := identityJourneyLimiter{}
	identity := identityhttp.Handler{
		RegisterUseCase: identityapp.Register{Repository: users, Policy: identityJourneyRegistration(true), Params: params},
		LoginUseCase:    identityapp.Login{Repository: users, Limiter: limiter, DummyPasswordHash: dummyHash},
		Users:           users,
		Sessions:        sessions,
		Profile:         identityapp.UpdateProfile{Repository: users},
		Password:        identityapp.ChangePassword{Repository: users, Sessions: sessions, Limiter: limiter, Params: params},
	}
	sharing := &identityJourneySharing{}
	router := newTestAPIRouter(apiRouterOptions{Identity: identity, Sharing: sharinghttp.Handler{
		Change: sharingapp.Change{Repository: sharing}, Read: sharingapp.Read{Repository: sharing},
		ActorID: func(ctx context.Context) (int64, bool) {
			principal, ok := identityhttp.PrincipalFrom(ctx)
			return principal.User.ID, ok
		},
	}})

	csrf, sessionCookie := "", ""
	send := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			media := "application/json"
			if method == http.MethodPatch {
				media = "application/merge-patch+json"
			}
			request.Header.Set("Content-Type", media)
		}
		if method != http.MethodGet && method != http.MethodHead {
			request.AddCookie(&http.Cookie{Name: identityhttp.CSRFCookieName, Value: csrf})
			request.Header.Set(identityhttp.CSRFHeaderName, csrf)
		}
		if authenticated && sessionCookie != "" {
			request.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: sessionCookie})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		for _, cookie := range response.Result().Cookies() {
			switch cookie.Name {
			case identityhttp.CSRFCookieName:
				csrf = cookie.Value
			case identityhttp.SessionCookieName:
				if cookie.MaxAge < 0 {
					sessionCookie = ""
				} else {
					sessionCookie = cookie.Value
				}
			}
		}
		return response
	}
	decode := func(response *httptest.ResponseRecorder, status int) identityJourneyEnvelope {
		t.Helper()
		if response.Code != status {
			t.Fatalf("%s response = %d %s", response.Header().Get("Content-Type"), response.Code, response.Body.String())
		}
		if status == http.StatusNoContent {
			if response.Body.Len() != 0 {
				t.Fatalf("204 included a body: %s", response.Body.String())
			}
			return identityJourneyEnvelope{}
		}
		if response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("success content type = %q", response.Header().Get("Content-Type"))
		}
		if response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("identity cache policy = %q", response.Header().Get("Cache-Control"))
		}
		var envelope identityJourneyEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if !envelope.Success || envelope.Code != status {
			t.Fatalf("invalid success envelope: %s", response.Body.String())
		}
		return envelope
	}
	links := func(data json.RawMessage) rest.Links {
		t.Helper()
		var representation struct {
			Links rest.Links `json:"links"`
		}
		if err := json.Unmarshal(data, &representation); err != nil {
			t.Fatal(err)
		}
		if representation.Links["self"].Href == "" {
			t.Fatalf("representation lacks self link: %s", data)
		}
		return representation.Links
	}
	follow := func(link rest.Link, body string, authenticated bool, status int) identityJourneyEnvelope {
		t.Helper()
		if link.Href == "" || link.Method == "" {
			t.Fatalf("invalid advertised link: %+v", link)
		}
		response := send(link.Method, link.Href, body, authenticated)
		return decode(response, status)
	}

	root := decode(send(http.MethodGet, "/api/v1", "", false), http.StatusOK)
	rootLinks := links(root.Data)
	if len(rootLinks) != 4 || rootLinks["authentication-options"].Method != http.MethodGet || rootLinks["users"].Method != http.MethodPost || rootLinks["sessions"].Method != http.MethodPost {
		t.Fatalf("anonymous entry links = %+v", rootLinks)
	}
	options := follow(rootLinks["authentication-options"], "", false, http.StatusOK)
	optionsLinks := links(options.Data)
	follow(optionsLinks["self"], "", false, http.StatusOK)
	registration := `{"email":"journey@example.test","password":"secret-pass-1","accept_terms":true,"locale":"en"}`
	createdResponse := send(rootLinks["users"].Method, rootLinks["users"].Href, registration, false)
	created := decode(createdResponse, http.StatusCreated)
	createdLinks := links(created.Data)
	userURL := createdLinks["self"].Href
	if userURL == "" {
		t.Fatal("registration omitted its user resource link")
	}
	if got := createdResponse.Header().Get("Location"); got != userURL {
		t.Fatalf("registration Location = %q, want %q", got, userURL)
	}
	if got := send(http.MethodGet, userURL, "", false); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous registration Location read = %d %s, want 401", got.Code, got.Body.String())
	}
	duplicate := send(optionsLinks["users"].Method, optionsLinks["users"].Href, registration, false)
	if duplicate.Code != http.StatusConflict || duplicate.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("duplicate registration = %d %q %s", duplicate.Code, duplicate.Header().Get("Content-Type"), duplicate.Body.String())
	}
	login := `{"email":"journey@example.test","password":"secret-pass-1"}`
	session := follow(rootLinks["sessions"], login, false, http.StatusCreated)
	if sessionCookie == "" {
		t.Fatal("login did not issue a session cookie")
	}
	sessionLinks := links(session.Data)
	follow(sessionLinks["self"], "", true, http.StatusOK)
	if got := send(http.MethodGet, userURL, "", true); got.Code != http.StatusOK {
		t.Fatalf("authenticated registration Location read = %d %s, want 200", got.Code, got.Body.String())
	}
	follow(sessionLinks["session"], "", true, http.StatusOK)
	follow(sessionLinks["user"], "", true, http.StatusOK)
	secondSession := follow(optionsLinks["sessions"], login, false, http.StatusCreated)
	secondLinks := links(secondSession.Data)
	follow(secondLinks["self"], "", true, http.StatusOK)
	follow(secondLinks["session"], "", true, http.StatusOK)
	follow(secondLinks["user"], "", true, http.StatusOK)
	thirdSession := follow(createdLinks["sessions"], login, false, http.StatusCreated)
	thirdLinks := links(thirdSession.Data)
	follow(thirdLinks["self"], "", true, http.StatusOK)
	follow(thirdLinks["session"], "", true, http.StatusOK)
	follow(thirdLinks["user"], "", true, http.StatusOK)
	follow(createdLinks["me"], "", true, http.StatusOK)

	authenticatedRoot := decode(send(http.MethodGet, "/api/v1", "", true), http.StatusOK)
	authenticatedLinks := links(authenticatedRoot.Data)
	if len(authenticatedLinks) != 7 {
		t.Fatalf("authenticated entry links = %+v", authenticatedLinks)
	}
	if authenticatedLinks["home"].Href != "/api/v1/me/home" || authenticatedLinks["home"].Method != http.MethodGet {
		t.Fatalf("home relation = %+v", authenticatedLinks["home"])
	}
	current := follow(authenticatedLinks["current-session"], "", true, http.StatusOK)
	currentLinks := links(current.Data)
	follow(currentLinks["session"], "", true, http.StatusOK)
	follow(currentLinks["user"], "", true, http.StatusOK)
	me := follow(authenticatedLinks["me"], "", true, http.StatusOK)
	meLinks := links(me.Data)
	follow(meLinks["self"], "", true, http.StatusOK)
	nullSharing := send(http.MethodPut, "/api/v1/me/sharing", `{"enabled":null}`, true)
	if nullSharing.Code != http.StatusUnprocessableEntity || nullSharing.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("null sharing choice = %d %q %s, want problem 422", nullSharing.Code, nullSharing.Header().Get("Content-Type"), nullSharing.Body.String())
	}
	sharing.mu.Lock()
	sharingWasChanged := sharing.setting.UserID != 0
	sharing.mu.Unlock()
	if sharingWasChanged {
		t.Fatal("null sharing choice reached the sharing service")
	}
	patchedMe := follow(meLinks["update"], `{"locale":"vi"}`, true, http.StatusOK)
	links(patchedMe.Data)
	list := follow(meLinks["sessions"], "", true, http.StatusOK)
	var members []json.RawMessage
	if err := json.Unmarshal(list.Data, &members); err != nil || len(members) != 3 {
		t.Fatalf("session collection = %s err=%v", list.Data, err)
	}
	var revokedOne bool
	for _, member := range members {
		memberLinks := links(member)
		follow(memberLinks["self"], "", true, http.StatusOK)
		follow(memberLinks["session"], "", true, http.StatusOK)
		follow(memberLinks["user"], "", true, http.StatusOK)
		var currentMember struct {
			Current bool `json:"current"`
		}
		if err := json.Unmarshal(member, &currentMember); err != nil {
			t.Fatal(err)
		}
		if !currentMember.Current && !revokedOne {
			if memberLinks["revoke"].Method != http.MethodDelete || memberLinks["revoke"].Href == "" {
				t.Fatalf("non-current session has invalid revoke relation: %+v", memberLinks)
			}
			follow(memberLinks["revoke"], "", true, http.StatusNoContent)
			revokedOne = true
		}
	}
	if !revokedOne {
		t.Fatal("session collection omitted a revocable non-current session")
	}
	follow(meLinks["revoke-other-sessions"], "", true, http.StatusNoContent)
	follow(meLinks["password"], `{"current_password":"secret-pass-1","new_password":"replacement-pass-2"}`, true, http.StatusNoContent)
	sharingSetting := follow(meLinks["sharing"], "", true, http.StatusOK)
	sharingLinks := links(sharingSetting.Data)
	follow(sharingLinks["self"], "", true, http.StatusOK)
	updatedSharing := follow(meLinks["update-sharing"], `{"enabled":true}`, true, http.StatusOK)
	updatedSharingLinks := links(updatedSharing.Data)
	follow(updatedSharingLinks["update"], `{"enabled":false}`, true, http.StatusOK)
	follow(updatedSharingLinks["me"], "", true, http.StatusOK)
	if currentLinks["revoke"].Method != http.MethodDelete || sessionLinks["revoke"].Href != currentLinks["revoke"].Href {
		t.Fatalf("session revoke links differ: current=%+v login=%+v", currentLinks, sessionLinks)
	}
	follow(currentLinks["revoke"], "", true, http.StatusNoContent)
	if response := send(http.MethodGet, "/api/v1/me", "", true); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session still authenticates: %d %s", response.Code, response.Body.String())
	}
}

func TestIdentityConformance(t *testing.T) {
	router := routerWith(newIdentityJourneySessions(newIdentityJourneyRepository()))

	for _, test := range []struct {
		method string
		path   string
		body   string
		media  string
		status int
	}{
		{http.MethodGet, "/api/v1/me", "", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/me/sessions", "", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/me/sharing", "", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/sessions/current", "", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/sessions/1", "", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/me/sessions/1", "", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/users/1", "", "", http.StatusUnauthorized},
		{http.MethodPatch, "/api/v1/me", `{"locale":"en"}`, "application/merge-patch+json", http.StatusUnauthorized},
		{http.MethodPut, "/api/v1/me/password", `{"current_password":"current-pass","new_password":"next-pass-1"}`, "application/json", http.StatusUnauthorized},
		{http.MethodPut, "/api/v1/me/sharing", `{"enabled":true}`, "application/json", http.StatusUnauthorized},
		{http.MethodDelete, "/api/v1/me/sessions?scope=others", "", "", http.StatusUnauthorized},
		{http.MethodDelete, "/api/v1/me/sessions/1", "", "", http.StatusUnauthorized},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.media != "" {
				request.Header.Set("Content-Type", test.media)
			}
			if test.method != http.MethodGet {
				request.AddCookie(&http.Cookie{Name: identityhttp.CSRFCookieName, Value: strings.Repeat("a", domain.EncodedOpaqueTokenLength)})
				request.Header.Set(identityhttp.CSRFHeaderName, strings.Repeat("a", domain.EncodedOpaqueTokenLength))
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("Content-Type") != "application/problem+json" || response.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("response = %d %q %q %s", response.Code, response.Header().Get("Content-Type"), response.Header().Get("Cache-Control"), response.Body.String())
			}
			if test.status == http.StatusUnauthorized && response.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("401 response omitted WWW-Authenticate")
			}
			var problem rest.Problem
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || problem.Status != test.status || problem.Type == "" || problem.Instance != strings.Split(test.path, "?")[0] {
				t.Fatalf("problem = %+v err=%v", problem, err)
			}
		})
	}

	options := httptest.NewRecorder()
	router = newTestAPIRouter(apiRouterOptions{Identity: identityhttp.Handler{RegisterUseCase: identityapp.Register{Policy: identityJourneyRegistration(true)}, Sessions: stubSessions{}}})
	router.ServeHTTP(options, httptest.NewRequest(http.MethodGet, "/api/v1/authentication-options", nil))
	if options.Code != http.StatusOK || options.Header().Get("Content-Type") != "application/json" || options.Header().Get("Cache-Control") != "private, no-store" || options.Header().Get("ETag") != "" {
		t.Fatalf("authentication options = %d %q %q %s", options.Code, options.Header().Get("Content-Type"), options.Header().Get("Cache-Control"), options.Body.String())
	}
	var optionsEnvelope identityJourneyEnvelope
	if err := json.Unmarshal(options.Body.Bytes(), &optionsEnvelope); err != nil || !optionsEnvelope.Success || optionsEnvelope.Code != http.StatusOK || len(optionsEnvelope.Data) == 0 {
		t.Fatalf("authentication options envelope = %s err=%v", options.Body.String(), err)
	}
	logoutRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", nil)
	logoutRequest.AddCookie(&http.Cookie{Name: identityhttp.CSRFCookieName, Value: strings.Repeat("a", domain.EncodedOpaqueTokenLength)})
	logoutRequest.Header.Set(identityhttp.CSRFHeaderName, strings.Repeat("a", domain.EncodedOpaqueTokenLength))
	logout := httptest.NewRecorder()
	router.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent || logout.Body.Len() != 0 {
		t.Fatalf("logout response = %d %s", logout.Code, logout.Body.String())
	}
}

var (
	_ identityapp.RegisterRepository = (*identityJourneyRepository)(nil)
	_ identityapp.LoginRepository    = (*identityJourneyRepository)(nil)
	_ identityapp.ProfileRepository  = (*identityJourneyRepository)(nil)
	_ identityapp.PasswordRepository = (*identityJourneyRepository)(nil)
	_ identityhttp.UserLookup        = (*identityJourneyRepository)(nil)
	_ identityhttp.SessionService    = (*identityJourneySessions)(nil)
	_ identityapp.SessionRepository  = (*identityJourneySessions)(nil)
	_ sharingapp.Repository          = (*identityJourneySharing)(nil)
	_ sharingapp.Reader              = (*identityJourneySharing)(nil)
)
