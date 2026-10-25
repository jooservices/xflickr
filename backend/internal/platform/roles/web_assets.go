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
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if browserNavigation(r) &&
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
		if browserNavigation(r) {
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

func browserNavigation(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}
