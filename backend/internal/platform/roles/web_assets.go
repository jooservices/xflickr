package roles

import (
	"encoding/json"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

func serveWebAssets(router *chi.Mux, root string) {
	if root == "" {
		return
	}
	files := http.FileServer(http.Dir(root))
	// Pages such as /keys and /admin/* share their path with an API route, which
	// would otherwise win over the NotFound fallback for browser navigations.
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSPANavigation(r) &&
				path.Ext(r.URL.Path) == "" &&
				!strings.HasPrefix(r.URL.Path, "/assets/") {
				http.ServeFile(w, r, filepath.Join(root, "index.html")) // #nosec G703 -- this path uses a fixed index filename beneath the configured asset root.
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if path.Ext(r.URL.Path) != "" {
			files.ServeHTTP(w, r)
			return
		}
		if isSPANavigation(r) || isPageFallback(r) {
			http.ServeFile(w, r, filepath.Join(root, "index.html")) // #nosec G703 -- this path uses a fixed index filename beneath the configured asset root.
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "not_found"})
		}
	})
}

// isSPANavigation reports a browser page navigation that the SPA should answer.
// /flickr-accounts (the OAuth callback answers browsers with a redirect) and
// the other endpoints that are never pages stay with the backend.
func isSPANavigation(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") &&
		!isBackendOnlyPath(r.URL.Path)
}

// isPageFallback reports a request for an unknown path from a client that did
// not ask for JSON (curl and health probes send no Accept or */*), so "/" and
// SPA routes such as /register return the HTML shell instead of a 404.
func isPageFallback(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		!strings.Contains(accept, "json") &&
		!isBackendOnlyPath(r.URL.Path)
}

func isBackendOnlyPath(requestPath string) bool {
	for _, prefix := range []string{"/auth", "/flickr-accounts", "/healthz"} {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}
