package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

const maxIdentityBodyBytes = 16 * 1024

// RequestBodyLimit caps identity request bodies before generated decoders
// allocate them, keeping oversized credentials away from password hashing.
func RequestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := identityBodyPolicy(r.Method, r.URL.Path); ok {
			r.Body = http.MaxBytesReader(w, r.Body, maxIdentityBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// LegacyIdentityRoutes makes removed identity URLs fail before CSRF and route
// authentication middleware can obscure their 404 response.
func LegacyIdentityRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacy := r.URL.Path == "/auth" || strings.HasPrefix(r.URL.Path, "/auth/")
		if (r.URL.Path == "/me" || strings.HasPrefix(r.URL.Path, "/me/")) && r.URL.Path != "/me/crawl" && !strings.HasPrefix(r.URL.Path, "/me/crawl/") &&
			r.URL.Path != "/me/contacts" && !strings.HasPrefix(r.URL.Path, "/me/contacts/") {
			legacy = true
		}
		if legacy {
			w.Header().Set("Cache-Control", privateNoStore)
			_ = rest.WriteProblem(w, r, rest.NewProblem("not-found", "Resource not found", nil), responseTraceID(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Contract enforces identity media types, writable fields, and query allowlists.
func Contract(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isIdentityResourcePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if problem := validateIdentityQuery(r); problem != nil {
			writeIdentityProblem(w, r, problem)
			return
		}
		if r.Method == http.MethodDelete && (r.ContentLength > 0 || len(r.TransferEncoding) > 0) {
			writeIdentityProblem(w, r, rest.NewProblem("malformed-request", "DELETE requests must not include a body", nil))
			return
		}
		if bodyPolicy, ok := identityBodyPolicy(r.Method, r.URL.Path); ok {
			if problem := validateIdentityBody(r, bodyPolicy); problem != nil {
				writeIdentityProblem(w, r, problem)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// PrivateIdentityCache marks identity responses before authentication and CSRF
// middleware can return an error.
func PrivateIdentityCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isIdentityResourcePath(r.URL.Path) {
			w.Header().Set("Cache-Control", privateNoStore)
		}
		next.ServeHTTP(w, r)
	})
}

type requestBodyPolicy struct {
	media    string
	fields   []string
	required []string
	merge    bool
}

func isIdentityResourcePath(path string) bool {
	return path == "/api/v1/authentication-options" || path == "/api/v1/users" || strings.HasPrefix(path, "/api/v1/users/") ||
		path == "/api/v1/sessions" || strings.HasPrefix(path, "/api/v1/sessions/") || path == "/api/v1/me" || strings.HasPrefix(path, "/api/v1/me/")
}

func identityBodyPolicy(method, path string) (requestBodyPolicy, bool) {
	switch {
	case method == http.MethodPost && path == "/api/v1/users":
		return requestBodyPolicy{media: "application/json", fields: []string{"email", "password", "accept_terms", "locale"}, required: []string{"email", "password", "accept_terms", "locale"}}, true
	case method == http.MethodPost && path == "/api/v1/sessions":
		return requestBodyPolicy{media: "application/json", fields: []string{"email", "password"}, required: []string{"email", "password"}}, true
	case method == http.MethodPatch && path == "/api/v1/me":
		return requestBodyPolicy{media: "application/merge-patch+json", fields: []string{"locale"}, merge: true}, true
	case method == http.MethodPut && path == "/api/v1/me/password":
		return requestBodyPolicy{media: "application/json", fields: []string{"current_password", "new_password"}, required: []string{"current_password", "new_password"}}, true
	case method == http.MethodPut && path == "/api/v1/me/sharing":
		return requestBodyPolicy{media: "application/json", fields: []string{"enabled"}, required: []string{"enabled"}}, true
	default:
		return requestBodyPolicy{}, false
	}
}

func validateIdentityQuery(r *http.Request) *rest.Problem {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return rest.NewProblem("malformed-request", "Malformed query string", nil)
	}
	allowed := map[string]bool{}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/me/sessions" {
		allowed["limit"], allowed["cursor"] = true, true
	}
	if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/me/sessions" {
		allowed["scope"] = true
		if len(query["scope"]) != 1 || query.Get("scope") != "others" {
			return rest.NewProblem("validation-error", "The only supported session scope is others", rest.FieldErrors{"scope": {"Must be others"}})
		}
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return rest.NewProblem("validation-error", "Unsupported or repeated query parameter", rest.FieldErrors{key: {"Parameter is not supported"}})
		}
	}
	return nil
}

func validateIdentityBody(r *http.Request, policy requestBodyPolicy) *rest.Problem {
	if problem := validateIdentityMediaType(r, policy.media); problem != nil {
		return problem
	}
	raw, problem := readIdentityBody(r)
	if problem != nil {
		return problem
	}
	if policy.merge {
		return validateIdentityMergePatch(r, raw, policy.fields)
	}
	return validateIdentityFields(raw, policy)
}

func validateIdentityMediaType(r *http.Request, expected string) *rest.Problem {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != expected {
		return rest.NewProblem("unsupported-media-type", "Use "+expected, nil)
	}
	return nil
}

func readIdentityBody(r *http.Request) ([]byte, *rest.Problem) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(maxIdentityBodyBytes+1)))
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil || len(raw) > maxIdentityBodyBytes {
		return nil, rest.NewProblem("malformed-request", "Request body cannot be read or exceeds its limit", nil)
	}
	return raw, nil
}

func validateIdentityMergePatch(r *http.Request, raw []byte, fields []string) *rest.Problem {
	clone := r.Clone(r.Context())
	clone.Body = io.NopCloser(bytes.NewReader(raw))
	if _, err := rest.ReadMergePatch(clone, maxIdentityBodyBytes, fields...); err != nil {
		var problem *rest.Problem
		if errors.As(err, &problem) {
			return problem
		}
		return rest.NewProblem("malformed-request", "Malformed request body", nil)
	}
	return nil
}

func validateIdentityFields(raw []byte, policy requestBodyPolicy) *rest.Problem {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return rest.NewProblem("malformed-request", "Malformed JSON", nil)
	}
	if fields == nil {
		return rest.NewProblem("validation-error", "Expected a JSON object", nil)
	}
	permitted := make(map[string]bool, len(policy.fields))
	for _, field := range policy.fields {
		permitted[field] = true
	}
	fieldErrors := rest.FieldErrors{}
	for field := range fields {
		if !permitted[field] {
			fieldErrors[field] = []string{"Field is not writable"}
		}
	}
	for _, field := range policy.required {
		value, ok := fields[field]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			fieldErrors[field] = []string{"Field is required"}
		}
	}
	if len(fieldErrors) > 0 {
		return rest.NewProblem("validation-error", "Request fields are invalid", fieldErrors)
	}
	return nil
}

func writeIdentityProblem(w http.ResponseWriter, r *http.Request, problem *rest.Problem) {
	_ = rest.WriteProblem(w, r, problem, responseTraceID(r.Context()))
}
