package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

func TestLegacyIdentityRoutes(t *testing.T) {
	for _, tc := range []struct {
		path string
		want int
	}{
		{path: "/auth", want: http.StatusNotFound},
		{path: "/auth/login", want: http.StatusNotFound},
		{path: "/me", want: http.StatusNotFound},
		{path: "/me/sessions/12", want: http.StatusNotFound},
		{path: "/me/crawl", want: http.StatusNoContent},
		{path: "/me/crawl/pause", want: http.StatusNoContent},
		{path: "/me/contacts/12", want: http.StatusNoContent},
		{path: "/api/v1/me", want: http.StatusNoContent},
	} {
		t.Run(tc.path, func(t *testing.T) {
			called := false
			handler := LegacyIdentityRoutes(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
			if called != (tc.want == http.StatusNoContent) {
				t.Fatalf("downstream called = %v", called)
			}
			if tc.want == http.StatusNotFound {
				if response.Header().Get("Cache-Control") != privateNoStore || !strings.Contains(response.Header().Get("Content-Type"), "application/problem+json") {
					t.Fatalf("legacy headers = %v", response.Header())
				}
				var problem rest.Problem
				if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || problem.Status != http.StatusNotFound || problem.Type != "/problems/not-found" {
					t.Fatalf("legacy problem = %+v, err = %v", problem, err)
				}
			}
		})
	}
}

func TestRequestBodyLimit(t *testing.T) {
	called := false
	handler := RequestBodyLimit(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		called = true
		_, err := io.ReadAll(request.Body)
		if err == nil {
			t.Error("oversized request body was not capped")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(strings.Repeat("x", 16*1024+1))))
	if !called || response.Code != http.StatusNoContent {
		t.Fatalf("called = %v, status = %d", called, response.Code)
	}
}

func TestIdentityContractAllowsKnownRequests(t *testing.T) {
	for _, tc := range []struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
	}{
		{name: "outside identity", method: http.MethodPut, path: "/api/v1/keys/1", body: "not identity"},
		{name: "authentication options", method: http.MethodGet, path: "/api/v1/authentication-options"},
		{name: "user read", method: http.MethodGet, path: "/api/v1/users/1"},
		{name: "session read", method: http.MethodGet, path: "/api/v1/sessions/current"},
		{name: "me read", method: http.MethodGet, path: "/api/v1/me"},
		{name: "user registration", method: http.MethodPost, path: "/api/v1/users", contentType: "application/json", body: `{"email":"u@example.test","password":"password123","accept_terms":true,"locale":"en"}`},
		{name: "session creation", method: http.MethodPost, path: "/api/v1/sessions", contentType: "application/json", body: `{"email":"u@example.test","password":"password123"}`},
		{name: "merge patch", method: http.MethodPatch, path: "/api/v1/me", contentType: "application/merge-patch+json", body: `{"locale":"en"}`},
		{name: "password replacement", method: http.MethodPut, path: "/api/v1/me/password", contentType: "application/json", body: `{"current_password":"old-password","new_password":"new-password"}`},
		{name: "sharing replacement", method: http.MethodPut, path: "/api/v1/me/sharing", contentType: "application/json", body: `{"enabled":true}`},
		{name: "session page", method: http.MethodGet, path: "/api/v1/me/sessions?limit=50&cursor=opaque"},
		{name: "session revoke", method: http.MethodDelete, path: "/api/v1/me/sessions/1"},
		{name: "other session revoke", method: http.MethodDelete, path: "/api/v1/me/sessions?scope=others"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := Contract(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || !called {
				t.Fatalf("status = %d, called = %v, body = %s", response.Code, called, response.Body.String())
			}
		})
	}
}

func TestIdentityContractRejectsInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
		setup       func(*http.Request)
		want        int
	}{
		{name: "malformed query", method: http.MethodGet, path: "/api/v1/me/sessions?limit=%zz", want: http.StatusBadRequest},
		{name: "unknown query", method: http.MethodGet, path: "/api/v1/me?debug=true", want: http.StatusUnprocessableEntity},
		{name: "repeated query", method: http.MethodGet, path: "/api/v1/me/sessions?limit=2&limit=3", want: http.StatusUnprocessableEntity},
		{name: "missing session scope", method: http.MethodDelete, path: "/api/v1/me/sessions", want: http.StatusUnprocessableEntity},
		{name: "unsupported session scope", method: http.MethodDelete, path: "/api/v1/me/sessions?scope=current", want: http.StatusUnprocessableEntity},
		{name: "delete body", method: http.MethodDelete, path: "/api/v1/sessions/current", body: "{}", want: http.StatusBadRequest},
		{name: "delete transfer body", method: http.MethodDelete, path: "/api/v1/sessions/current", setup: func(request *http.Request) { request.TransferEncoding = []string{"chunked"} }, want: http.StatusBadRequest},
		{name: "missing content type", method: http.MethodPut, path: "/api/v1/me/sharing", body: `{"enabled":true}`, want: http.StatusUnsupportedMediaType},
		{name: "wrong media type", method: http.MethodPatch, path: "/api/v1/me", contentType: "application/json", body: `{"locale":"en"}`, want: http.StatusUnsupportedMediaType},
		{name: "malformed json", method: http.MethodPost, path: "/api/v1/sessions", contentType: "application/json", body: "{", want: http.StatusBadRequest},
		{name: "non object json", method: http.MethodPut, path: "/api/v1/me/sharing", contentType: "application/json", body: "null", want: http.StatusUnprocessableEntity},
		{name: "unknown and required fields", method: http.MethodPut, path: "/api/v1/me/sharing", contentType: "application/json", body: `{"unexpected":true}`, want: http.StatusUnprocessableEntity},
		{name: "unknown merge patch field", method: http.MethodPatch, path: "/api/v1/me", contentType: "application/merge-patch+json", body: `{"timezone":"UTC"}`, want: http.StatusUnprocessableEntity},
		{name: "malformed merge patch", method: http.MethodPatch, path: "/api/v1/me", contentType: "application/merge-patch+json", body: "{", want: http.StatusBadRequest},
		{name: "oversized JSON", method: http.MethodPost, path: "/api/v1/users", contentType: "application/json", body: strings.Repeat("x", 16*1024+1), want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := Contract(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			if tc.setup != nil {
				tc.setup(request)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want || called {
				t.Fatalf("status = %d, called = %v, body = %s", response.Code, called, response.Body.String())
			}
			if !strings.Contains(response.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
			}
		})
	}
}

type brokenRequestBody struct{}

func (brokenRequestBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (brokenRequestBody) Close() error             { return nil }

func TestIdentityContractHandlesUnreadableBodyAndPrivateCache(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/me/sharing", nil)
	request.Header.Set("Content-Type", "application/json")
	request.Body = brokenRequestBody{}
	response := httptest.NewRecorder()
	Contract(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("downstream called") })).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unreadable-body status = %d", response.Code)
	}

	called := false
	cache := PrivateIdentityCache(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	private := httptest.NewRecorder()
	cache.ServeHTTP(private, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if !called || private.Code != http.StatusNoContent || private.Header().Get("Cache-Control") != privateNoStore {
		t.Fatalf("private response: called=%v status=%d headers=%v", called, private.Code, private.Header())
	}
	public := httptest.NewRecorder()
	cache.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil))
	if public.Header().Get("Cache-Control") != "" {
		t.Fatalf("non-identity cache headers = %v", public.Header())
	}
}
