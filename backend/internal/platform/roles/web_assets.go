package roles

import (
	"net/http"
	"path"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

func serveWebAssets(router *chi.Mux, root string) {
	if root == "" {
		return
	}
	files := http.FileServer(http.Dir(root))
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		if path.Ext(r.URL.Path) != "" {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html")) // #nosec G703 -- this path uses a fixed index filename beneath the configured asset root.
	})
}
