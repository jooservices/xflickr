package roles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	identitydomain "github.com/jooservices/xflickr/internal/identity/domain"
)

func TestDiscoveryPermissions(t *testing.T) {
	for _, role := range []identitydomain.Role{identitydomain.RoleUser, identitydomain.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v1", nil)
			r.AddCookie(&http.Cookie{Name: "xf_session", Value: "valid"})
			routerWith(roleSessions{role: role}).ServeHTTP(w, r)
			var body struct {
				Data struct {
					Links map[string]struct {
						Href   string `json:"href"`
						Method string `json:"method"`
					} `json:"links"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || body.Data.Links["self"].Href != "/api/v1" {
				t.Fatal(w.Code, w.Body.String())
			}
			if body.Data.Links["users"].Href != "/api/v1/users" || body.Data.Links["users"].Method != http.MethodPost ||
				body.Data.Links["sessions"].Href != "/api/v1/sessions" || body.Data.Links["sessions"].Method != http.MethodPost ||
				body.Data.Links["me"].Href != "/api/v1/me" || body.Data.Links["me"].Method != http.MethodGet {
				t.Fatal(body)
			}
		})
	}
}

func TestDiscoveryAnonymousAndInvalidSession(t *testing.T) {
	router := routerWith(roleSessions{role: identitydomain.RoleUser})
	for _, tt := range []struct {
		path, cookie string
		status       int
	}{{"/api/v1", "", 200}, {"/api/v1?unknown=1", "", 422}, {"/api/v1?unknown=%GG", "", 400}, {"/api/v1", "bad", 401}, {"/api/v1", "valid", 200}} {
		r := httptest.NewRequest("GET", tt.path, nil)
		if tt.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "xf_session", Value: tt.cookie})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatal(tt, w.Code, w.Body.String())
		}
		if tt.status == 200 && tt.cookie == "" {
			if strings.Contains(w.Body.String(), `"keys"`) || !strings.Contains(w.Body.String(), `"authentication-options"`) {
				t.Fatal(w.Body.String())
			}
		}
		if tt.status == 401 && w.Header().Get("WWW-Authenticate") == "" {
			t.Fatal(w.Header())
		}
	}
}

func TestDiscoveryMiddlewareEdges(t *testing.T) {
	calls := 0
	next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) { calls++; return nil, nil }
	for _, tt := range []struct {
		sessions identityhttp.SessionService
		cookie   string
		want     int
	}{{nil, "valid", 500}, {roleSessions{role: identitydomain.RoleUser}, "", 401}, {failingDiscoverySession{}, "valid", 500}, {slidingDiscoverySession{}, "valid", 200}} {
		r := httptest.NewRequest("GET", "/api/v1", nil)
		r.AddCookie(&http.Cookie{Name: "xf_session", Value: tt.cookie})
		w := httptest.NewRecorder()
		_, err := discoveryAuthentication(tt.sessions)(next, "GetAPIEntry")(r.Context(), w, r, nil)
		if err != nil || w.Code != tt.want {
			t.Fatal(w.Code, err)
		}
		if tt.want == 200 && len(w.Result().Cookies()) == 0 {
			t.Fatal("session slide not propagated")
		}
	}
	r := httptest.NewRequest("GET", "/healthz", nil)
	_, err := discoveryAuthentication(nil)(next, "GetHealth")(r.Context(), httptest.NewRecorder(), r, nil)
	if err != nil || calls != 2 {
		t.Fatal(calls, err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}}))
	if discoveryTraceID(ctx) == "" {
		t.Fatal("lost trace")
	}
}

type failingDiscoverySession struct{ stubSessions }

func (failingDiscoverySession) Authenticate(context.Context, string) (identityapp.Authenticated, error) {
	return identityapp.Authenticated{}, errors.New("private failure")
}

type slidingDiscoverySession struct{ stubSessions }

func (slidingDiscoverySession) Authenticate(context.Context, string) (identityapp.Authenticated, error) {
	return identityapp.Authenticated{Slid: true, User: identitydomain.User{ID: 1}, Session: identitydomain.Session{ExpiresAt: time.Now().Add(time.Hour)}}, nil
}
